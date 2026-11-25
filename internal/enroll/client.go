package enroll

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Result is what the hub assigned to this operator.
type Result struct {
	Address string
	HubKey  string
	WG      string
	DNS     []string
	MTU     int
}

// Enroll sends our PUBLIC key and the invite's token to the hub. The TLS connection is pinned to the
// certificate fingerprint carried in the invite, so a man in the middle cannot read the token.
func Enroll(ctx context.Context, inv Invite, pub string, now time.Time) (Result, error) {
	if inv.Expired(now) {
		return Result{}, errors.New("this invite has expired. Ask for a new one")
	}
	if err := validKey(pub); err != nil {
		return Result{}, err
	}
	tr := &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13,
		// We verify by certificate pin, not by CA. VerifyConnection (unlike VerifyPeerCertificate) also runs
		// on resumed sessions, so the pin can never be skipped. There is no session cache either.
		InsecureSkipVerify: true, // #nosec G402 -- verified by certificate pin in VerifyConnection below
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("no certificate")
			}
			h := sha256.Sum256(cs.PeerCertificates[0].Raw)
			if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(h[:])), []byte(inv.Pin)) != 1 {
				return errors.New("pin mismatch")
			}
			return nil
		},
	}}
	cl := &http.Client{Transport: tr, Timeout: 15 * time.Second}
	body, _ := json.Marshal(enrollReq{Token: inv.Token, Pub: pub})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+inv.Enroll+"/v1/enroll", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := cl.Do(req)
	if err != nil {
		if bytes.Contains([]byte(err.Error()), []byte("pin mismatch")) {
			return Result{}, errors.New("this doesn't look like the right NiBLR server (its identity didn't match the invite). Don't continue; tell whoever gave you the invite")
		}
		return Result{}, fmt.Errorf("couldn't reach the NiBLR server. Check your internet connection and try again (%v)", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusForbidden:
		return Result{}, errors.New("this invite isn't valid any more: it was already used or has expired. Ask for a new one")
	case http.StatusTooManyRequests:
		return Result{}, errors.New("too many attempts. Wait a minute and try again")
	default:
		return Result{}, fmt.Errorf("the NiBLR server had a problem (%s). Try again in a moment", bytes.TrimSpace(b))
	}
	var r enrollResp
	if err := json.Unmarshal(b, &r); err != nil || r.Address == "" || validKey(r.HubKey) != nil {
		return Result{}, errors.New("the NiBLR server sent an unexpected answer. Tell whoever runs it")
	}
	if r.HubKey != inv.HubKey {
		return Result{}, errors.New("the server's key doesn't match your invite. Don't continue; tell whoever gave you the invite")
	}
	return Result(r), nil
}
