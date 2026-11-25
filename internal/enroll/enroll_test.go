package enroll

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"niblr/internal/boot"
)

type fakePeers struct {
	mu   sync.Mutex
	got  map[string]string
	fail bool
}

func (f *fakePeers) Add(_ context.Context, pub, ip string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("wg broke")
	}
	if f.got == nil {
		f.got = map[string]string{}
	}
	f.got[pub] = ip
	return nil
}

func hubKey(t *testing.T) string { _, p, _ := boot.GenKey(); return p }

type rig struct {
	srv   *Server
	hs    *httptest.Server
	inv   func(token string) Invite
	peers *fakePeers
	dir   string
	now   time.Time
}

func newRig(t *testing.T) *rig {
	t.Helper()
	dir := t.TempDir()
	cert, pin, err := SelfSignedCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	hk := hubKey(t)
	pe := &fakePeers{}
	s, err := NewServer(dir, Config{HubKey: hk, WG: "1.2.3.4:51000", DNS: []string{"1.1.1.1"}, MTU: 1420, Prefix: "10.10.9"}, pe)
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{srv: s, peers: pe, dir: dir, now: time.Unix(1_700_000_000, 0)}
	s.Now = func() time.Time { return r.now }
	r.hs = httptest.NewUnstartedServer(s.Handler())
	r.hs.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}
	r.hs.StartTLS()
	t.Cleanup(r.hs.Close)
	r.inv = func(tok string) Invite {
		return Invite{Enroll: strings.TrimPrefix(r.hs.URL, "https://"), WG: "1.2.3.4:51000", HubKey: hk, Pin: pin, Token: tok, Expires: r.now.Add(time.Hour).Unix()}
	}
	return r
}

func (r *rig) join(t *testing.T, tok string) (Result, error) {
	_, pub, _ := boot.GenKey()
	return Enroll(context.Background(), r.inv(tok), pub, r.now)
}

func TestInviteRoundTripAndDamage(t *testing.T) {
	r := newRig(t)
	s, err := r.inv("tok").Encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeInvite("  " + s + "\n")
	if err != nil || got.Token != "tok" {
		t.Fatalf("%v %+v", err, got)
	}
	for _, bad := range []string{"", "hello", "niblr1:", "niblr1:!!!", s[:len(s)-10], "niblr2:" + s[7:]} {
		if _, err := DecodeInvite(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, err := (Invite{}).Encode(); err == nil {
		t.Error("incomplete invite must not encode")
	}
}

func TestHappyPathAssignsUniqueAddressesAndRegistersPeer(t *testing.T) {
	r := newRig(t)
	t1, _, _ := r.srv.Mint("alice", time.Hour)
	t2, _, _ := r.srv.Mint("bob", time.Hour)
	a, err := r.join(t, t1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.join(t, t2)
	if err != nil {
		t.Fatal(err)
	}
	if a.Address != "10.10.9.2/32" || b.Address != "10.10.9.3/32" {
		t.Fatalf("addresses %s %s", a.Address, b.Address)
	}
	if len(r.peers.got) != 2 || a.MTU != 1420 || a.DNS[0] != "1.1.1.1" {
		t.Fatalf("peers=%v result=%+v", r.peers.got, a)
	}
}

func TestTokenIsSingleUse(t *testing.T) {
	r := newRig(t)
	tok, _, _ := r.srv.Mint("alice", time.Hour)
	if _, err := r.join(t, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := r.join(t, tok); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("second machine must be refused, got %v", err)
	}
	if len(r.peers.got) != 1 {
		t.Fatal("refused attempt must not add a peer")
	}
}

func TestSameMachineRetryGetsSameAnswer(t *testing.T) {
	r := newRig(t)
	tok, _, _ := r.srv.Mint("alice", time.Hour)
	_, pub, _ := boot.GenKey()
	a, err := Enroll(context.Background(), r.inv(tok), pub, r.now)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Enroll(context.Background(), r.inv(tok), pub, r.now) // lost reply, client retries
	if err != nil || a.Address != b.Address {
		t.Fatalf("retry: %v %v %v", err, a.Address, b.Address)
	}
	if len(r.srv.Operators()) != 1 {
		t.Fatal("retry must not create a second operator")
	}
}

func TestExpiredAndUnknownTokensRefused(t *testing.T) {
	r := newRig(t)
	tok, _, _ := r.srv.Mint("alice", time.Minute)
	r.now = r.now.Add(2 * time.Minute)
	inv := r.inv(tok)
	inv.Expires = r.now.Add(time.Hour).Unix() // client-side expiry is not the control: the hub decides
	_, pub, _ := boot.GenKey()
	if _, err := Enroll(context.Background(), inv, pub, r.now); err == nil {
		t.Fatal("expired token accepted")
	}
	if _, err := r.join(t, "never-minted"); err == nil {
		t.Fatal("unknown token accepted")
	}
	old := r.inv("x")
	old.Expires = r.now.Add(-time.Second).Unix()
	if _, err := Enroll(context.Background(), old, pub, r.now); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("client must say expired: %v", err)
	}
}

func TestWrongPinIsRefusedBeforeAnythingIsSent(t *testing.T) {
	r := newRig(t)
	tok, _, _ := r.srv.Mint("alice", time.Hour)
	inv := r.inv(tok)
	inv.Pin = strings.Repeat("0", 64)
	_, pub, _ := boot.GenKey()
	_, err := Enroll(context.Background(), inv, pub, r.now)
	if err == nil || !strings.Contains(err.Error(), "right NiBLR server") {
		t.Fatalf("got %v", err)
	}
	if len(r.srv.Operators()) != 0 {
		t.Fatal("must not enroll")
	}
}

func TestWrongHubKeyInReplyIsRefused(t *testing.T) {
	r := newRig(t)
	tok, _, _ := r.srv.Mint("alice", time.Hour)
	inv := r.inv(tok)
	inv.HubKey = hubKey(t) // invite says a different hub key than the server returns
	_, pub, _ := boot.GenKey()
	if _, err := Enroll(context.Background(), inv, pub, r.now); err == nil || !strings.Contains(err.Error(), "doesn't match") {
		t.Fatalf("got %v", err)
	}
}

func TestPeerAddFailureDoesNotBurnTheToken(t *testing.T) {
	r := newRig(t)
	tok, _, _ := r.srv.Mint("alice", time.Hour)
	r.peers.fail = true
	if _, err := r.join(t, tok); err == nil {
		t.Fatal("expected failure")
	}
	r.peers.fail = false
	if _, err := r.join(t, tok); err != nil {
		t.Fatalf("token must still work after a hub-side failure: %v", err)
	}
}

func TestGuessingIsRateLimited(t *testing.T) {
	r := newRig(t)
	good, _, _ := r.srv.Mint("alice", time.Hour)
	for i := 0; i < maxFailsPerMinute; i++ {
		r.join(t, "guess")
	}
	if _, err := r.join(t, good); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Fatalf("must be locked out even for a good token: %v", err)
	}
	r.now = r.now.Add(61 * time.Second)
	if _, err := r.join(t, good); err != nil {
		t.Fatalf("lockout must clear: %v", err)
	}
}

func TestStateSurvivesRestartAndPeersRestore(t *testing.T) {
	r := newRig(t)
	tok, _, _ := r.srv.Mint("alice", time.Hour)
	a, err := r.join(t, tok)
	if err != nil {
		t.Fatal(err)
	}
	pe2 := &fakePeers{}
	s2, err := NewServer(r.dir, r.srv.cfg, pe2)
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.Restore(context.Background()); err != nil || len(pe2.got) != 1 {
		t.Fatalf("restore: %v %v", err, pe2.got)
	}
	s2.Now = func() time.Time { return r.now }
	tok2, _, _ := s2.Mint("bob", time.Hour)
	_, pub, _ := boot.GenKey()
	resp, code, _ := s2.enroll(context.Background(), enrollReq{Token: tok2, Pub: pub})
	if code != 200 || resp.Address == a.Address {
		t.Fatalf("address reused after restart: %v %v", code, resp.Address)
	}
	// the secret itself is never written to disk
	raw := readFile(t, r.dir+"/enroll.json")
	if strings.Contains(raw, tok) {
		t.Fatal("token stored in the clear")
	}
}

func TestBadPublicKeyRejected(t *testing.T) {
	r := newRig(t)
	tok, _, _ := r.srv.Mint("alice", time.Hour)
	_, code, _ := r.srv.enroll(context.Background(), enrollReq{Token: tok, Pub: "nope"})
	if code != 400 {
		t.Fatalf("code %d", code)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInviteMintedByAnotherProcessIsNotLostToStaleMemory(t *testing.T) {
	r := newRig(t)
	t1, _, _ := r.srv.Mint("alice", time.Hour)
	other, err := NewServer(r.dir, r.srv.cfg, &fakePeers{}) // a second process, e.g. `niblrd invite`
	if err != nil {
		t.Fatal(err)
	}
	t2, _, _ := other.Mint("bob", time.Hour)
	if _, err := r.join(t, t1); err != nil { // daemon enrolls alice, holding a stale copy that lacks bob's token
		t.Fatal(err)
	}
	if _, err := r.join(t, t2); err != nil {
		t.Fatalf("bob's token was lost: %v", err)
	}
	t3, _, _ := other.Mint("carol", time.Hour) // and the other direction: alice's enrollment survived
	_ = t3
	if got := len(other.Operators()); got != 2 {
		t.Fatalf("operators = %d, want 2", got)
	}
}
