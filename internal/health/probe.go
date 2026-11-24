// Package health implements pool.Checker for real hubs: a recent WireGuard handshake AND an
// end-to-end probe through the exit. A tunnel-only probe is not enough: an exit that lost its
// return route still answers pings on the tunnel address, so the hub kept sending flows into it.
package health

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"niblr/internal/pool"
)

// ParseHandshake parses `wg show <iface> latest-handshakes` ("<pubkey>\t<unix>\n" per peer) and
// returns the most recent handshake time. ok is false when there is no handshake yet.
func ParseHandshake(out string) (t time.Time, ok bool) {
	var best int64
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, err := strconv.ParseInt(f[len(f)-1], 10, 64)
		if err != nil || v <= 0 {
			continue
		}
		if v > best {
			best = v
		}
	}
	if best == 0 {
		return time.Time{}, false
	}
	return time.Unix(best, 0), true
}

// Prober checks exits on a Linux hub (needs root: ping -m sets a packet mark).
type Prober struct {
	// MaxHandshakeAge is a loose sanity bound. WireGuard only handshakes about every 2 minutes and
	// keepalives do not update it, so this must be well above 120s.
	MaxHandshakeAge time.Duration
	ProbeSrc        string // operator-subnet address on the hub, e.g. 10.10.9.1
	ProbeTarget     string // something that reliably answers ping, e.g. 1.1.1.1
	Now             func() time.Time
	Run             func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// NewProber returns a Prober with production defaults.
func NewProber(src, target string) *Prober {
	return &Prober{
		MaxHandshakeAge: 200 * time.Second,
		ProbeSrc:        src,
		ProbeTarget:     target,
		Now:             time.Now,
		Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		},
	}
}

// Healthy implements pool.Checker.
func (p *Prober) Healthy(ctx context.Context, e pool.Exit) bool {
	out, err := p.Run(ctx, "wg", "show", e.Iface, "latest-handshakes")
	if err != nil {
		return false
	}
	hs, ok := ParseHandshake(string(out))
	if !ok || p.Now().Sub(hs) > p.MaxHandshakeAge {
		return false
	}
	// Marked ping from the operator subnet: routed by the exit's fwmark table, out via the exit's
	// NAT, and back via the exit's return route to the operator subnet.
	_, err = p.Run(ctx, "ping", "-c1", "-W2", "-m", strconv.Itoa(e.ID), "-I", p.ProbeSrc, p.ProbeTarget)
	return err == nil
}

// String is used in logs.
func (p *Prober) String() string { return fmt.Sprintf("probe(%s -> %s)", p.ProbeSrc, p.ProbeTarget) }
