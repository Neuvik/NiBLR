// Package provider is the cloud contract. Keep it small: the more a provider must do, the more
// AWS-shaped it gets. Replace is NOT part of it: "new IP, same slot" is create-new then delete-old,
// done provider-independently by the reconciler (and GCP has no real in-place re-IP).
package provider

import (
	"context"
	"time"
)

// Tag keys every provider must apply atomically AT CREATE TIME (never tag after the fact: a crash
// between create and tag leaves an orphan that is billed, undiscoverable, and attributable to us).
const (
	TagEnv  = "niblr-env"
	TagSlot = "niblr-slot"
	TagRole = "niblr-role" // "exit"
)

// ExitSpec describes an exit to create.
type ExitSpec struct {
	Env       string
	Slot      int    // exit id: doubles as conntrack mark and route table suffix
	Token     string // idempotency token: creating twice with the same token must not make two exits
	Region    string
	Size      string
	CloudInit string // boot config: brings up the WireGuard tunnel that dials the hub
	// Meta is small non-secret data stored with the exit AT CREATE TIME (as tags), so the hub can rebuild
	// its view after a restart with no state file. Keys are short lowercase words, e.g. "pub".
	Meta map[string]string
}

// State of an exit as the provider reports it.
type State string

const (
	StatePending State = "pending"
	StateRunning State = "running"
	StateBroken  State = "broken" // exists but unusable (e.g. stopped): still costs money, must be deleted and replaced
	StateGone    State = "gone"   // terminating/terminated: ignore, already on its way out
)

// Exit is an exit as discovered from the provider by tag (there is no state file).
type Exit struct {
	ID       string // provider id
	Env      string
	Slot     int
	PublicIP string
	State    State
	Created  time.Time
	Meta     map[string]string
}

// Provider is implemented once per cloud.
type Provider interface {
	Name() string
	CreateExit(ctx context.Context, spec ExitSpec) (Exit, error)
	DeleteExit(ctx context.Context, id string) error
	// ListExits finds every exit for env by tag. Must include pending ones, so strays are seen.
	ListExits(ctx context.Context, env string) ([]Exit, error)
}
