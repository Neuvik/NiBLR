package reconcile

import (
	"context"
	"strings"
	"testing"

	"niblr/internal/provider"
	"niblr/internal/provider/fake"
)

var tmpl = Template{Env: "eng1", Region: "us-east-2", Size: "nano", CloudInit: "#cloud-config"}

func live(t *testing.T, f *fake.Fake) map[int]int {
	t.Helper()
	out := map[int]int{}
	l, _ := f.ListExits(context.Background(), "eng1")
	for _, e := range l {
		if e.State != provider.StateGone {
			out[e.Slot]++
		}
	}
	return out
}

func TestConvergeBuildsThePoolFromNothing(t *testing.T) {
	f := fake.New()
	if _, _, err := Converge(context.Background(), f, tmpl, 3, "r1"); err != nil {
		t.Fatal(err)
	}
	got := live(t, f)
	if got[1] != 1 || got[2] != 1 || got[3] != 1 || len(got) != 3 {
		t.Fatalf("%v", got)
	}
	// Second round does nothing.
	a, _, err := Converge(context.Background(), f, tmpl, 3, "r2")
	if err != nil || !a.Empty() {
		t.Fatalf("expected a no-op, got %+v err=%v", a, err)
	}
}

func TestRetryingTheSameRoundCannotDuplicateExits(t *testing.T) {
	// Simulates: create succeeded at the cloud but the response was lost, so the caller retries.
	f := fake.New()
	a := Actions{CreateSlots: []int{1, 2}}
	if _, err := Apply(context.Background(), f, tmpl, "r1", a); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), f, tmpl, "r1", a); err != nil {
		t.Fatal(err)
	}
	if got := live(t, f); got[1] != 1 || got[2] != 1 {
		t.Fatalf("retry duplicated exits: %v", got)
	}
}

func TestPartialFailureStillRepairsWhatItCanAndReportsTheRest(t *testing.T) {
	f := fake.New()
	f.FailCreateAfter = 1 // quota: only one create succeeds
	_, _, err := Converge(context.Background(), f, tmpl, 3, "r1")
	if err == nil || !strings.Contains(err.Error(), "quota") {
		t.Fatalf("expected the quota error to be reported, got %v", err)
	}
	if n := len(live(t, f)); n != 1 {
		t.Fatalf("expected 1 exit created before the quota hit, got %d", n)
	}
	// Quota is lifted: the next round fills only the missing slots.
	f.FailCreateAfter = -1
	a, _, err := Converge(context.Background(), f, tmpl, 3, "r2")
	if err != nil || len(a.CreateSlots) != 2 {
		t.Fatalf("a=%+v err=%v", a, err)
	}
	if n := len(live(t, f)); n != 3 {
		t.Fatalf("pool should be full, got %d", n)
	}
}

func TestStraysAreCleanedUpAndScaleDownWorks(t *testing.T) {
	f := fake.New()
	ctx := context.Background()
	Converge(ctx, f, tmpl, 4, "r1")
	// A stray left by some half-finished earlier attempt: slot 2 created again without our token.
	f.CreateExit(ctx, provider.ExitSpec{Env: "eng1", Slot: 2})
	if _, _, err := Converge(ctx, f, tmpl, 2, "r2"); err != nil {
		t.Fatal(err)
	}
	got := live(t, f)
	if len(got) != 2 || got[1] != 1 || got[2] != 1 {
		t.Fatalf("expected exactly slots 1 and 2, one each: %v", got)
	}
}

func TestDeleteFailureDoesNotBlockCreates(t *testing.T) {
	f := fake.New()
	p := failDelete{f}
	a := Actions{DeleteIDs: []string{"x"}, CreateSlots: []int{1}}
	created, err := Apply(context.Background(), p, tmpl, "r1", a)
	if err == nil || len(created) != 1 {
		t.Fatalf("created=%v err=%v", created, err)
	}
}

type failDelete struct{ *fake.Fake }

func (failDelete) DeleteExit(context.Context, string) error { return errTest }

var errTest = errString("api throttled")

type errString string

func (e errString) Error() string { return string(e) }
