// Package pool decides which exits are healthy. It knows nothing about nftables, WireGuard or clouds:
// callers supply a Checker, which keeps this logic unit-testable.
package pool

import (
	"context"
	"sort"
	"sync"
)

// Exit is one slot in the pool. ID doubles as the conntrack mark and route table suffix.
type Exit struct {
	ID    int
	Iface string // hub-side WireGuard interface, e.g. "wg-e1"
}

// Checker reports whether an exit can really carry traffic end to end.
type Checker interface {
	Healthy(ctx context.Context, e Exit) bool
}

// CheckerFunc adapts a function to Checker.
type CheckerFunc func(ctx context.Context, e Exit) bool

func (f CheckerFunc) Healthy(ctx context.Context, e Exit) bool { return f(ctx, e) }

// Evaluate checks every non-drained exit concurrently and returns the healthy ids, sorted.
// Drained exits leave the rotation but are not touched: established flows keep their exit.
func Evaluate(ctx context.Context, exits []Exit, c Checker, drained map[int]bool) []int {
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		healthy []int
	)
	for _, e := range exits {
		if drained[e.ID] {
			continue
		}
		wg.Add(1)
		go func(e Exit) {
			defer wg.Done()
			if c.Healthy(ctx, e) {
				mu.Lock()
				healthy = append(healthy, e.ID)
				mu.Unlock()
			}
		}(e)
	}
	wg.Wait()
	sort.Ints(healthy)
	return healthy
}

// Equal reports whether two sorted id lists are identical.
func Equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
