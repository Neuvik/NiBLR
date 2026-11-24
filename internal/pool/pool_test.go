package pool

import (
	"context"
	"reflect"
	"testing"
)

func fake(up map[int]bool) Checker {
	return CheckerFunc(func(_ context.Context, e Exit) bool { return up[e.ID] })
}

var three = []Exit{{ID: 1, Iface: "wg-e1"}, {ID: 2, Iface: "wg-e2"}, {ID: 3, Iface: "wg-e3"}}

func TestEvaluateHealthySortedAndFiltered(t *testing.T) {
	got := Evaluate(context.Background(), three, fake(map[int]bool{1: true, 2: false, 3: true}), nil)
	if !reflect.DeepEqual(got, []int{1, 3}) {
		t.Fatalf("got %v", got)
	}
}

func TestEvaluateNoneHealthy(t *testing.T) {
	got := Evaluate(context.Background(), three, fake(nil), nil)
	if len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestEvaluateDrainedExitsAreSkippedNotProbed(t *testing.T) {
	probed := map[int]bool{}
	c := CheckerFunc(func(_ context.Context, e Exit) bool { probed[e.ID] = true; return true })
	// Single goroutine per exit but map writes race: run with one exit drained and check results only.
	got := Evaluate(context.Background(), three[:1], c, map[int]bool{1: true})
	if len(got) != 0 {
		t.Fatalf("drained exit must not be in the pool: %v", got)
	}
	if probed[1] {
		t.Fatalf("drained exit should not be probed")
	}
}

func TestEvaluateRunsConcurrently(t *testing.T) {
	// 3 exits each waiting for the others to start; would deadlock if checks ran serially.
	start := make(chan struct{}, 3)
	release := make(chan struct{})
	c := CheckerFunc(func(_ context.Context, e Exit) bool {
		start <- struct{}{}
		<-release
		return true
	})
	done := make(chan []int)
	go func() { done <- Evaluate(context.Background(), three, c, nil) }()
	for i := 0; i < 3; i++ {
		<-start
	}
	close(release)
	if got := <-done; len(got) != 3 {
		t.Fatalf("got %v", got)
	}
}

func TestEqual(t *testing.T) {
	if !Equal([]int{1, 2}, []int{1, 2}) || Equal([]int{1}, []int{1, 2}) || Equal([]int{1, 3}, []int{1, 2}) || !Equal(nil, []int{}) {
		t.Fatal("Equal misbehaves")
	}
}
