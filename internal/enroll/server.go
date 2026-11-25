package enroll

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Peers adds an operator to the hub's operator WireGuard interface.
type Peers interface {
	Add(ctx context.Context, pub, ip string) error
}

type tokenRec struct {
	Name    string `json:"name"`
	Expires int64  `json:"exp"`
	UsedBy  string `json:"used_by,omitempty"` // operator public key that consumed it
}

type operatorRec struct {
	Name    string `json:"name"`
	Pub     string `json:"pub"`
	IP      string `json:"ip"`
	Created int64  `json:"created"`
}

type state struct {
	Tokens    map[string]tokenRec `json:"tokens"` // key: sha256(token) hex. The token itself is never stored.
	Operators []operatorRec       `json:"operators"`
}

// Config is what the hub tells every enrolled operator.
type Config struct {
	HubKey string   // hub WireGuard public key
	WG     string   // host:port operators dial
	DNS    []string // resolvers, reached through the tunnel
	MTU    int
	Prefix string // operator network, e.g. "10.10.9"; .1 is the hub
}

type Server struct {
	cfg   Config
	peers Peers
	path  string
	Now   func() time.Time

	mu    sync.Mutex
	st    state
	fails []time.Time
}

// NewServer loads (or creates) state in dir.
func NewServer(dir string, cfg Config, peers Peers) (*Server, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, peers: peers, path: filepath.Join(dir, "enroll.json"), Now: time.Now, st: state{Tokens: map[string]tokenRec{}}}
	if b, err := os.ReadFile(s.path); err == nil {
		if err := json.Unmarshal(b, &s.st); err != nil {
			return nil, fmt.Errorf("enrollment state %s is damaged: %w", s.path, err)
		}
		if s.st.Tokens == nil {
			s.st.Tokens = map[string]tokenRec{}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return s, nil
}

// lockState serialises with other processes (e.g. `niblrd invite` while the daemon runs) and re-reads the
// file, so neither can overwrite the other's changes with a stale copy. Call unlock when done.
func (s *Server) lockState() (unlock func(), err error) {
	s.mu.Lock()
	f, err := os.OpenFile(s.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if err := flock(f); err != nil {
		_ = f.Close()
		s.mu.Unlock()
		return nil, err
	}
	unlock = func() { funlock(f); _ = f.Close(); s.mu.Unlock() }
	if b, err := os.ReadFile(s.path); err == nil {
		var st state
		if json.Unmarshal(b, &st) == nil {
			if st.Tokens == nil {
				st.Tokens = map[string]tokenRec{}
			}
			s.st = st
		}
	}
	return unlock, nil
}

func (s *Server) save() error {
	b, _ := json.MarshalIndent(s.st, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func hashToken(t string) string { h := sha256.Sum256([]byte(t)); return hex.EncodeToString(h[:]) }

// Mint creates a single-use token for name, valid for ttl, and returns the secret (shown once).
func (s *Server) Mint(name string, ttl time.Duration) (token string, expires time.Time, err error) {
	if name == "" {
		return "", time.Time{}, errors.New("an invite needs a name (who is it for?)")
	}
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", time.Time{}, err
	}
	token = base64.RawURLEncoding.EncodeToString(b[:])
	expires = s.Now().Add(ttl)
	unlock, err := s.lockState()
	if err != nil {
		return "", time.Time{}, err
	}
	defer unlock()
	s.st.Tokens[hashToken(token)] = tokenRec{Name: name, Expires: expires.Unix()}
	return token, expires, s.save()
}

// Restore re-adds every enrolled operator to the interface (after a hub restart).
func (s *Server) Restore(ctx context.Context) error {
	unlock, err := s.lockState()
	if err != nil {
		return err
	}
	ops := append([]operatorRec(nil), s.st.Operators...)
	unlock()
	var errs []error
	for _, o := range ops {
		if err := s.peers.Add(ctx, o.Pub, o.IP); err != nil {
			errs = append(errs, fmt.Errorf("restore %s: %w", o.Name, err))
		}
	}
	return errors.Join(errs...)
}

type enrollReq struct {
	Token string `json:"token"`
	Pub   string `json:"public_key"`
}
type enrollResp struct {
	Address string   `json:"address"`
	HubKey  string   `json:"hub_public_key"`
	WG      string   `json:"endpoint"`
	DNS     []string `json:"dns"`
	MTU     int      `json:"mtu"`
}

const maxFailsPerMinute = 10

func (s *Server) tooManyFailures() bool {
	cut := s.Now().Add(-time.Minute)
	keep := s.fails[:0]
	for _, t := range s.fails {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	s.fails = keep
	return len(s.fails) >= maxFailsPerMinute
}

// Handler serves POST /v1/enroll.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/enroll", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "use POST", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		var req enrollReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		resp, code, msg := s.enroll(r.Context(), req)
		if code != http.StatusOK {
			http.Error(w, msg, code)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	return mux
}

func (s *Server) enroll(ctx context.Context, req enrollReq) (enrollResp, int, string) {
	if err := validKey(req.Pub); err != nil {
		return enrollResp{}, http.StatusBadRequest, "bad public key"
	}
	unlock, err := s.lockState()
	if err != nil {
		return enrollResp{}, http.StatusInternalServerError, "hub could not read its state; try again"
	}
	defer unlock()
	if s.tooManyFailures() {
		return enrollResp{}, http.StatusTooManyRequests, "too many attempts, wait a minute"
	}
	key := hashToken(req.Token)
	var rec tokenRec
	found := false
	for k, v := range s.st.Tokens { // constant-time compare on the hash; map scan keeps timing independent of which token matches
		if subtle.ConstantTimeCompare([]byte(k), []byte(key)) == 1 {
			rec, found = v, true
		}
	}
	if !found || s.Now().Unix() >= rec.Expires {
		s.fails = append(s.fails, s.Now())
		return enrollResp{}, http.StatusForbidden, "invite not valid"
	}
	if rec.UsedBy != "" {
		if rec.UsedBy != req.Pub { // someone else already used it
			s.fails = append(s.fails, s.Now())
			return enrollResp{}, http.StatusForbidden, "invite not valid"
		}
		for _, o := range s.st.Operators { // same machine retrying after a lost reply: give the same answer
			if o.Pub == req.Pub {
				return s.reply(o.IP), http.StatusOK, ""
			}
		}
	}
	ip, err := s.allocate()
	if err != nil {
		return enrollResp{}, http.StatusServiceUnavailable, "no free addresses"
	}
	if err := s.peers.Add(ctx, req.Pub, ip); err != nil {
		return enrollResp{}, http.StatusInternalServerError, "hub could not add you; try again"
	}
	rec.UsedBy = req.Pub
	s.st.Tokens[key] = rec
	s.st.Operators = append(s.st.Operators, operatorRec{Name: rec.Name, Pub: req.Pub, IP: ip, Created: s.Now().Unix()})
	if err := s.save(); err != nil {
		return enrollResp{}, http.StatusInternalServerError, "hub could not save; try again"
	}
	return s.reply(ip), http.StatusOK, ""
}

func (s *Server) reply(ip string) enrollResp {
	return enrollResp{Address: ip + "/32", HubKey: s.cfg.HubKey, WG: s.cfg.WG, DNS: s.cfg.DNS, MTU: s.cfg.MTU}
}

func (s *Server) allocate() (string, error) {
	used := map[string]bool{}
	for _, o := range s.st.Operators {
		used[o.IP] = true
	}
	for n := 2; n <= 254; n++ {
		ip := fmt.Sprintf("%s.%d", s.cfg.Prefix, n)
		if !used[ip] {
			return ip, nil
		}
	}
	return "", errors.New("full")
}

// Revoke removes an operator by name from the saved state; the caller removes the peer from the interface.
func (s *Server) Operators() []string {
	if unlock, err := s.lockState(); err == nil {
		defer unlock()
	} else {
		return nil
	}
	var out []string
	for _, o := range s.st.Operators {
		out = append(out, fmt.Sprintf("%s %s", o.Name, o.IP))
	}
	return out
}

// SelfSignedCert loads or creates the enrollment certificate in dir and returns it with its sha256 pin.
func SelfSignedCert(dir string) (tls.Certificate, string, error) {
	cp, kp := filepath.Join(dir, "enroll.crt"), filepath.Join(dir, "enroll.key")
	if c, err := tls.LoadX509KeyPair(cp, kp); err == nil {
		h := sha256.Sum256(c.Certificate[0])
		return c, hex.EncodeToString(h[:]), nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return tls.Certificate{}, "", err
	}
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "niblr-hub"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(10, 0, 0),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	kb, _ := x509.MarshalECPrivateKey(k)
	if err := writePEM(cp, "CERTIFICATE", der, 0o644); err != nil {
		return tls.Certificate{}, "", err
	}
	if err := writePEM(kp, "EC PRIVATE KEY", kb, 0o600); err != nil {
		return tls.Certificate{}, "", err
	}
	c, err := tls.LoadX509KeyPair(cp, kp)
	h := sha256.Sum256(der)
	return c, hex.EncodeToString(h[:]), err
}
