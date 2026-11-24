package dataplane

import (
	"strings"
	"testing"
)

func TestRenderNoHealthyFailsClosed(t *testing.T) {
	out := RenderMarkRules("wg-op", nil)
	if !strings.Contains(out, `iifname "wg-op" ct state new drop`) {
		t.Fatalf("expected drop for new flows, got:\n%s", out)
	}
	if strings.Contains(out, "numgen") {
		t.Fatalf("must not pick an exit when none are healthy:\n%s", out)
	}
}

func TestRenderSingleExit(t *testing.T) {
	out := RenderMarkRules("wg-op", []int{2})
	if !strings.Contains(out, "numgen random mod 1 map { 0 : 2 }") {
		t.Fatalf("unexpected map:\n%s", out)
	}
}

func TestRenderSortsAndMapsAllExits(t *testing.T) {
	out := RenderMarkRules("wg-op", []int{3, 1, 2})
	if !strings.Contains(out, "numgen random mod 3 map { 0 : 1, 1 : 2, 2 : 3 }") {
		t.Fatalf("unexpected map:\n%s", out)
	}
}

func TestRenderDoesNotMutateInput(t *testing.T) {
	in := []int{3, 1, 2}
	RenderMarkRules("wg-op", in)
	if in[0] != 3 || in[1] != 1 || in[2] != 2 {
		t.Fatalf("input slice was reordered: %v", in)
	}
}

func TestRenderOnlyTouchesOperatorInterface(t *testing.T) {
	// Replies arrive on exit interfaces; restoring the mark there would loop them back to the exit.
	out := RenderMarkRules("wg-op", []int{1, 2})
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "add rule") && !strings.Contains(line, `iifname "wg-op"`) {
			t.Fatalf("rule not scoped to the operator interface: %q", line)
		}
	}
}

func TestRenderIsOneFlushedTransaction(t *testing.T) {
	out := RenderMarkRules("wg-op", []int{1})
	if strings.Index(out, "flush chain") > strings.Index(out, "add rule") {
		t.Fatalf("flush must precede the rules:\n%s", out)
	}
}
