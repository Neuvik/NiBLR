// Package reconcile turns "I want exits in slots 1..N" plus "here is what actually exists" into the
// minimal set of creates and deletes. It is pure, so every lifecycle rule is unit-tested; the daemon
// just runs it in a loop. This is also the garbage collector: strays are deleted, never left billing.
package reconcile

import (
	"sort"

	"niblr/internal/provider"
)

// Actions is what to do to reach the desired state.
type Actions struct {
	CreateSlots []int    // slots with no live exit
	DeleteIDs   []string // strays: duplicates, out-of-range slots
}

// Empty reports whether nothing needs doing.
func (a Actions) Empty() bool { return len(a.CreateSlots) == 0 && len(a.DeleteIDs) == 0 }

// Plan computes actions for desired slots 1..want given the exits found by tag.
//
// Rules:
//   - Exits in state gone are ignored (already dying). Broken ones (e.g. stopped, still billed) are deleted.
//   - Slots outside 1..want are deleted (scale down, or garbage).
//   - If a slot has several live exits (a retried create that was not idempotent), keep the OLDEST
//     (it is the one most likely already enrolled in the pool) and delete the rest.
//   - A slot with no live exit is created.
func Plan(want int, found []provider.Exit) Actions {
	bySlot := map[int][]provider.Exit{}
	var del []string
	for _, e := range found {
		if e.State == provider.StateGone {
			continue
		}
		if e.State == provider.StateBroken {
			del = append(del, e.ID) // delete it; its slot (if otherwise empty) is recreated below
			continue
		}
		if e.Slot < 1 || e.Slot > want {
			del = append(del, e.ID)
			continue
		}
		bySlot[e.Slot] = append(bySlot[e.Slot], e)
	}

	var create []int
	for slot := 1; slot <= want; slot++ {
		exits := bySlot[slot]
		if len(exits) == 0 {
			create = append(create, slot)
			continue
		}
		sort.SliceStable(exits, func(i, j int) bool {
			if !exits[i].Created.Equal(exits[j].Created) {
				return exits[i].Created.Before(exits[j].Created)
			}
			return exits[i].ID < exits[j].ID // deterministic tie-break
		})
		for _, extra := range exits[1:] {
			del = append(del, extra.ID)
		}
	}
	sort.Strings(del)
	return Actions{CreateSlots: create, DeleteIDs: del}
}
