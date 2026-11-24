package health

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"niblr/internal/pool"
)

func TestParseHandshake(t *testing.T) {
	cases := []struct {
		name string
		in   string
		ok   bool
		unix int64
	}{
		{"never", "KEY=\t0\n", false, 0},
		{"one", "KEY=\t1700000000\n", true, 1700000000},
		{"latest of many", "A=\t100\nB=\t300\nC=\t200\n", true, 300},
		{"empty", "", false, 0},
		{"garbage", "nonsense\n", false, 0},
	}
	for _, c := range cases {
		got, ok := ParseHandshake(c.in)
		if ok != c.ok || (ok && got.Unix() != c.unix) {
			t.Errorf("%s: got (%v,%v)", c.name, got, ok)
		}
	}
}

func proberWith(now time.Time, hs string, pingErr error) (*Prober, *[]string) {
	var calls []string
	return &Prober{
		MaxHandshakeAge: 200 * time.Second, ProbeSrc: "10.10.9.1", ProbeTarget: "1.1.1.1",
		Now: func() time.Time { return now },
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			if name == "wg" {
				return []byte(hs), nil
			}
			return nil, pingErr
		},
	}, &calls
}

var exit2 = pool.Exit{ID: 2, Iface: "wg-e2"}

func TestHealthyRequiresHandshakeAndPing(t *testing.T) {
	now := time.Unix(1000, 0)
	p, calls := proberWith(now, "K=\t950\n", nil)
	if !p.Healthy(context.Background(), exit2) {
		t.Fatal("expected healthy")
	}
	// The probe must be marked with the exit id and sourced from the operator subnet.
	if got := (*calls)[1]; got != "ping -c1 -W2 -m 2 -I 10.10.9.1 1.1.1.1" {
		t.Fatalf("unexpected probe command: %q", got)
	}
}

func TestUnhealthyWhenPingFails(t *testing.T) {
	// The bug this guards against: tunnel is up (fresh handshake) but traffic cannot get back.
	p, _ := proberWith(time.Unix(1000, 0), "K=\t990\n", errors.New("100% loss"))
	if p.Healthy(context.Background(), exit2) {
		t.Fatal("exit with a dead return path must not be healthy")
	}
}

func TestUnhealthyWithoutHandshakeSkipsPing(t *testing.T) {
	p, calls := proberWith(time.Unix(1000, 0), "K=\t0\n", nil)
	if p.Healthy(context.Background(), exit2) || len(*calls) != 1 {
		t.Fatalf("no handshake => unhealthy without probing; calls=%v", *calls)
	}
}

func TestStaleHandshakeIsUnhealthyButOldKeepaliveWindowIsFine(t *testing.T) {
	now := time.Unix(10000, 0)
	// 150s old: normal for an idle tunnel (rekey ~120s). Must NOT count as stale.
	if p, _ := proberWith(now, "K=\t9850\n", nil); !p.Healthy(context.Background(), exit2) {
		t.Fatal("a 150s-old handshake is normal and must be healthy")
	}
	if p, _ := proberWith(now, "K=\t9700\n", nil); p.Healthy(context.Background(), exit2) {
		t.Fatal("a 300s-old handshake is stale")
	}
}
