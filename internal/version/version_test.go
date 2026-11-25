package version

import (
	"strings"
	"testing"
)

func TestStringUsesInjectedValues(t *testing.T) {
	Version, Commit, Date = "v2026.11.0", "abcdef1", "2026-01-02"
	defer func() { Version, Commit, Date = "dev", "", "" }()
	if got, want := String("niblr"), "niblr v2026.11.0 (abcdef1, 2026-01-02)"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestStringWorksWithoutInjection(t *testing.T) {
	if got := String("niblrd"); !strings.HasPrefix(got, "niblrd dev") {
		t.Fatalf("got %q", got)
	}
}
