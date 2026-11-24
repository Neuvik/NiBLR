package reconcile

import (
	"reflect"
	"testing"
	"time"

	"niblr/internal/provider"
)

var t0 = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

func ex(id string, slot int, st provider.State, age int) provider.Exit {
	return provider.Exit{ID: id, Slot: slot, State: st, Created: t0.Add(time.Duration(age) * time.Minute)}
}

func TestEmptyWorldCreatesEverySlot(t *testing.T) {
	a := Plan(3, nil)
	if !reflect.DeepEqual(a.CreateSlots, []int{1, 2, 3}) || len(a.DeleteIDs) != 0 {
		t.Fatalf("%+v", a)
	}
}

func TestSteadyStateIsEmpty(t *testing.T) {
	a := Plan(2, []provider.Exit{ex("a", 1, provider.StateRunning, 0), ex("b", 2, provider.StatePending, 1)})
	if !a.Empty() {
		t.Fatalf("%+v", a)
	}
}

func TestGoneExitsAreIgnoredAndTheirSlotRecreated(t *testing.T) {
	a := Plan(2, []provider.Exit{ex("a", 1, provider.StateGone, 0), ex("b", 2, provider.StateRunning, 0)})
	if !reflect.DeepEqual(a.CreateSlots, []int{1}) || len(a.DeleteIDs) != 0 {
		t.Fatalf("%+v", a)
	}
}

func TestScaleDownDeletesHighSlots(t *testing.T) {
	a := Plan(1, []provider.Exit{ex("a", 1, provider.StateRunning, 0), ex("b", 2, provider.StateRunning, 0), ex("c", 3, provider.StateRunning, 0)})
	if !reflect.DeepEqual(a.DeleteIDs, []string{"b", "c"}) || len(a.CreateSlots) != 0 {
		t.Fatalf("%+v", a)
	}
}

func TestDuplicateSlotKeepsOldestDeletesRest(t *testing.T) {
	a := Plan(1, []provider.Exit{ex("new", 1, provider.StateRunning, 5), ex("old", 1, provider.StateRunning, 0), ex("mid", 1, provider.StatePending, 2)})
	if !reflect.DeepEqual(a.DeleteIDs, []string{"mid", "new"}) || len(a.CreateSlots) != 0 {
		t.Fatalf("%+v", a)
	}
}

func TestDuplicateTieBreakIsDeterministic(t *testing.T) {
	for i := 0; i < 20; i++ {
		a := Plan(1, []provider.Exit{ex("b", 1, provider.StateRunning, 0), ex("a", 1, provider.StateRunning, 0)})
		if !reflect.DeepEqual(a.DeleteIDs, []string{"b"}) {
			t.Fatalf("%+v", a)
		}
	}
}

func TestSlotZeroAndNegativeAreStrays(t *testing.T) {
	// An exit tagged with a bad slot (corruption, manual edit) must not be silently kept.
	a := Plan(1, []provider.Exit{ex("z", 0, provider.StateRunning, 0), ex("n", -1, provider.StateRunning, 0), ex("a", 1, provider.StateRunning, 0)})
	if !reflect.DeepEqual(a.DeleteIDs, []string{"n", "z"}) {
		t.Fatalf("%+v", a)
	}
}

func TestPlanIsIdempotentAfterApplying(t *testing.T) {
	found := []provider.Exit{ex("a", 1, provider.StateRunning, 0), ex("a2", 1, provider.StateRunning, 3), ex("x", 9, provider.StateRunning, 0)}
	a := Plan(2, found)
	// Apply: drop deleted, add created.
	var after []provider.Exit
	del := map[string]bool{}
	for _, id := range a.DeleteIDs {
		del[id] = true
	}
	for _, e := range found {
		if !del[e.ID] {
			after = append(after, e)
		}
	}
	for _, s := range a.CreateSlots {
		after = append(after, ex("new", s, provider.StatePending, 9))
	}
	if b := Plan(2, after); !b.Empty() {
		t.Fatalf("second plan must be empty, got %+v", b)
	}
}

func TestBrokenExitIsDeletedAndItsSlotRecreated(t *testing.T) {
	// A stopped instance keeps costing money; it must be cleaned up, not ignored.
	a := Plan(2, []provider.Exit{ex("s", 1, provider.StateBroken, 0), ex("b", 2, provider.StateRunning, 0)})
	if !reflect.DeepEqual(a.DeleteIDs, []string{"s"}) || !reflect.DeepEqual(a.CreateSlots, []int{1}) {
		t.Fatalf("%+v", a)
	}
}
