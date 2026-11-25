// Package enroll is how an operator joins: an admin mints an invite, the operator's machine generates its
// own WireGuard key and sends ONLY the public half plus a one-time token to the hub over pinned TLS.
package enroll

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const invitePrefix = "niblr1:"

// Invite is everything an operator's machine needs to reach and trust the hub. It is a bearer secret
// until used or expired: treat it like a one-time password.
type Invite struct {
	Enroll  string `json:"e"` // host:port of the enrollment API (TLS)
	WG      string `json:"w"` // host:port of the hub's operator WireGuard endpoint
	HubKey  string `json:"k"` // hub WireGuard public key
	Pin     string `json:"f"` // sha256 hex of the hub's enrollment certificate
	Token   string `json:"t"` // one-time secret
	Expires int64  `json:"x"` // unix seconds
}

func (i Invite) Encode() (string, error) {
	if err := i.check(); err != nil {
		return "", err
	}
	b, _ := json.Marshal(i)
	return invitePrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

func (i Invite) check() error {
	switch {
	case i.Enroll == "" || i.WG == "" || i.HubKey == "" || i.Pin == "" || i.Token == "":
		return errors.New("incomplete invite")
	case i.Expires <= 0:
		return errors.New("invite has no expiry")
	}
	return validKey(i.HubKey)
}

// DecodeInvite parses what the operator pasted. Errors are written for a person, not a programmer.
func DecodeInvite(s string) (Invite, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, invitePrefix) {
		return Invite{}, errors.New("that doesn't look like a NiBLR invite. It should start with \"niblr1:\". Copy the whole line you were sent")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(s, invitePrefix))
	if err != nil {
		return Invite{}, errors.New("the invite looks damaged (it may have been cut off when copied). Ask for a new one")
	}
	var i Invite
	if err := json.Unmarshal(raw, &i); err != nil || i.check() != nil {
		return Invite{}, errors.New("the invite looks damaged. Ask for a new one")
	}
	return i, nil
}

func (i Invite) Expired(now time.Time) bool { return now.Unix() >= i.Expires }

// validKey checks a WireGuard key: 32 bytes of standard base64.
func validKey(k string) error {
	raw, err := base64.StdEncoding.DecodeString(k)
	if err != nil || len(raw) != 32 {
		return fmt.Errorf("not a valid WireGuard key")
	}
	if _, err := ecdh.X25519().NewPublicKey(raw); err != nil {
		return fmt.Errorf("not a valid WireGuard key")
	}
	return nil
}
