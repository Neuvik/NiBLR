// niblrd is the hub daemon: it keeps the exit pool's nftables marking rules in sync with exit health.
//
// This is the first slice of the daemon (health + marking). Operator enrollment, rotation policy and
// the exit lifecycle (via providers) come next.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"niblr/internal/dataplane"
	"niblr/internal/health"
	"niblr/internal/pool"
)

func main() {
	var (
		opIface  = flag.String("operator-iface", "wg-op", "WireGuard interface operators connect to")
		exitsArg = flag.String("exits", "1,2", "comma-separated exit ids; exit N uses interface wg-eN")
		src      = flag.String("probe-src", "10.10.9.1", "hub address inside the operator subnet, used as the probe source")
		target   = flag.String("probe-target", "1.1.1.1", "host that answers ping, probed through each exit")
		interval = flag.Duration("interval", 2*time.Second, "health check interval")
		resync   = flag.Duration("resync", 30*time.Second, "re-apply the ruleset at least this often, to heal drift")
		drain    = flag.String("drain", "", "comma-separated exit ids to leave out of rotation (tunnels stay up)")
		dry      = flag.Bool("dry-run", false, "print the ruleset instead of applying it")
	)
	flag.Parse()

	exits, err := parseExits(*exitsArg)
	if err != nil {
		log.Fatalf("niblrd: --exits: %v", err)
	}
	drained, err := parseDrain(*drain)
	if err != nil {
		log.Fatalf("niblrd: --drain: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	prober := health.NewProber(*src, *target)
	var last []int
	var lastApplied time.Time
	first := true

	tick := time.NewTicker(*interval)
	defer tick.Stop()
	for {
		healthy := pool.Evaluate(ctx, exits, prober, drained)
		if first || !pool.Equal(healthy, last) || time.Since(lastApplied) >= *resync {
			rules := dataplane.RenderMarkRules(*opIface, healthy)
			if err := apply(ctx, rules, *dry); err != nil {
				log.Printf("niblrd: applying rules failed (will retry): %v", err)
			} else {
				if first || !pool.Equal(healthy, last) {
					log.Printf("healthy exits: %s", fmtIDs(healthy))
				}
				last, lastApplied, first = healthy, time.Now(), false
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func apply(ctx context.Context, rules string, dry bool) error {
	if dry {
		fmt.Print(rules)
		return nil
	}
	cmd := exec.CommandContext(ctx, "nft", "-f", "-")
	cmd.Stdin = strings.NewReader(rules)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(out.String()))
	}
	return nil
}

func parseExits(s string) ([]pool.Exit, error) {
	var exits []pool.Exit
	for _, p := range strings.Split(s, ",") {
		id, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("bad exit id %q (must be a positive integer)", p)
		}
		exits = append(exits, pool.Exit{ID: id, Iface: fmt.Sprintf("wg-e%d", id)})
	}
	return exits, nil
}

func parseDrain(s string) (map[int]bool, error) {
	m := map[int]bool{}
	if strings.TrimSpace(s) == "" {
		return m, nil
	}
	for _, p := range strings.Split(s, ",") {
		id, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return nil, fmt.Errorf("bad exit id %q", p)
		}
		m[id] = true
	}
	return m, nil
}

func fmtIDs(ids []int) string {
	if len(ids) == 0 {
		return "none"
	}
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = strconv.Itoa(id)
	}
	return strings.Join(s, " ")
}
