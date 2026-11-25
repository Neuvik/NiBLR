// Package hub keeps the hub's WireGuard interfaces in step with the exits the cloud says exist.
//
// It is level-triggered and stateless across restarts: the cloud (tags) says which exits should exist and
// which public key each one has; the kernel says which interfaces exist and which peer each has. Reconcile
// compares the two. Rotation is therefore just "delete the instance": the pool refills itself and the
// interface for that slot is rebuilt around the new exit's key.
package hub

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"niblr/internal/boot"
	"niblr/internal/provider"
	"niblr/internal/reconcile"
)

// Runner runs a command on the hub (injectable so the logic is testable without root).
type Runner interface {
	Run(ctx context.Context, argv ...string) (string, error)
}

type Op struct {
	Slot int
	Pub  string
}

// Plan is the interface-level work for one round. Down runs BEFORE Up, and the caller must take every
// Down slot out of the nft map before executing it (ordering invariant).
type Plan struct {
	Up   []Op
	Down []int
}

type Manager struct {
	P       provider.Provider
	Env     string
	Want    int
	Tmpl    reconcile.Template
	R       Runner
	KeyFile string // hub private key (root-only file)

	live map[int]string // slot -> peer public key, as of the last successful sync
}

var ifaceRe = regexp.MustCompile(`^wg-e([0-9]+)$`)

// WantedPeers is what the cloud says each slot's key should be. Pending exits count (their interface may
// exist early; health decides when they take traffic). With duplicate slots the oldest wins, matching the
// reconciler, which deletes the others.
func WantedPeers(found []provider.Exit) map[int]string {
	best := map[int]provider.Exit{}
	for _, e := range found {
		if e.Slot < 1 || e.Meta["pub"] == "" || (e.State != provider.StatePending && e.State != provider.StateRunning) {
			continue
		}
		if o, ok := best[e.Slot]; !ok || e.Created.Before(o.Created) || (e.Created.Equal(o.Created) && e.ID < o.ID) {
			best[e.Slot] = e
		}
	}
	out := map[int]string{}
	for s, e := range best {
		out[s] = e.Meta["pub"]
	}
	return out
}

// PlanSync diffs wanted against what the hub actually has.
func PlanSync(want, have map[int]string) Plan {
	var p Plan
	for s, hp := range have {
		wp, ok := want[s]
		if !ok || wp != hp {
			p.Down = append(p.Down, s)
		}
	}
	for s, wp := range want {
		if hp, ok := have[s]; !ok || hp != wp {
			p.Up = append(p.Up, Op{Slot: s, Pub: wp})
		}
	}
	sort.Ints(p.Down)
	sort.Slice(p.Up, func(i, j int) bool { return p.Up[i].Slot < p.Up[j].Slot })
	return p
}

// discover reads the kernel: every wg-e<N> interface and its peer key.
func (m *Manager) discover(ctx context.Context) (map[int]string, error) {
	out, err := m.R.Run(ctx, "wg", "show", "interfaces")
	if err != nil {
		return nil, fmt.Errorf("list wireguard interfaces: %w", err)
	}
	have := map[int]string{}
	for _, name := range strings.Fields(out) {
		sub := ifaceRe.FindStringSubmatch(name)
		if sub == nil {
			continue
		}
		slot, _ := strconv.Atoi(sub[1])
		peers, err := m.R.Run(ctx, "wg", "show", name, "peers")
		if err != nil {
			return nil, fmt.Errorf("read peers of %s: %w", name, err)
		}
		have[slot] = strings.TrimSpace(strings.SplitN(strings.TrimSpace(peers), "\n", 2)[0])
	}
	return have, nil
}

// Reconcile converges the exits in the cloud, then plans the hub's interface changes. It does not touch
// the hub; call Down then Up.
func (m *Manager) Reconcile(ctx context.Context, round string) (Plan, error) {
	_, _, cerr := reconcile.Converge(ctx, m.P, m.Tmpl, m.Want, round) // keep going on partial failure: one bad slot must not stall the rest
	found, err := m.P.ListExits(ctx, m.Env)
	if err != nil {
		return Plan{}, errors.Join(cerr, fmt.Errorf("list exits: %w", err))
	}
	have, err := m.discover(ctx)
	if err != nil {
		return Plan{}, errors.Join(cerr, err)
	}
	m.live = have
	return PlanSync(WantedPeers(found), have), cerr
}

func (m *Manager) runAll(ctx context.Context, cmds []boot.Cmd, tolerateFirst bool) error {
	for i, c := range cmds {
		if _, err := m.R.Run(ctx, c...); err != nil && !(tolerateFirst && i == 0) {
			return fmt.Errorf("%s: %w", strings.Join(c, " "), err)
		}
	}
	return nil
}

// Down tears interfaces down. Best effort: every step is attempted (a missing conntrack entry or an
// already-gone route is fine); a leftover is detected and retried by the next Reconcile.
func (m *Manager) Down(ctx context.Context, slots []int) error {
	var errs []error
	for _, s := range slots {
		cmds, err := boot.HubExitDown(s)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, c := range cmds {
			if _, err := m.R.Run(ctx, c...); err != nil {
				log.Printf("hub: teardown of slot %d: %s: %v (continuing)", s, strings.Join(c, " "), err)
			}
		}
		delete(m.live, s)
	}
	return errors.Join(errs...)
}

// Up builds interfaces. If a step fails the slot is torn down again so it can never sit half-built
// with its id in the pool.
func (m *Manager) Up(ctx context.Context, ops []Op) error {
	var errs []error
	for _, o := range ops {
		cmds, err := boot.HubExitUp(o.Slot, o.Pub, m.KeyFile)
		if err == nil {
			err = m.runAll(ctx, cmds, true)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("slot %d: %w", o.Slot, err))
			_ = m.Down(ctx, []int{o.Slot})
			continue
		}
		if m.live == nil {
			m.live = map[int]string{}
		}
		m.live[o.Slot] = o.Pub
	}
	return errors.Join(errs...)
}

// Live is the set of slots whose interface exists and may be health-checked.
func (m *Manager) Live() []int {
	var s []int
	for k := range m.live {
		s = append(s, k)
	}
	sort.Ints(s)
	return s
}
