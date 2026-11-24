// Package fake is an in-memory provider for tests. It is deliberately strict: it enforces the
// idempotency token and applies tags at create time, like a real cloud must.
package fake

import (
	"context"
	"fmt"
	"sync"
	"time"

	"niblr/internal/provider"
)

type Fake struct {
	mu     sync.Mutex
	exits  map[string]provider.Exit
	tokens map[string]string // token -> exit id
	next   int
	Now    func() time.Time
	// FailCreateAfter makes CreateExit fail once n creates have succeeded (-1 = never).
	FailCreateAfter int
	created         int
}

func New() *Fake {
	return &Fake{exits: map[string]provider.Exit{}, tokens: map[string]string{}, Now: time.Now, FailCreateAfter: -1}
}

func (f *Fake) Name() string { return "fake" }

func (f *Fake) CreateExit(_ context.Context, s provider.ExitSpec) (provider.Exit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s.Token != "" {
		if id, ok := f.tokens[s.Token]; ok {
			return f.exits[id], nil // idempotent replay
		}
	}
	if f.FailCreateAfter >= 0 && f.created >= f.FailCreateAfter {
		return provider.Exit{}, fmt.Errorf("fake: quota exceeded")
	}
	f.next++
	f.created++
	e := provider.Exit{
		ID: fmt.Sprintf("i-%04d", f.next), Env: s.Env, Slot: s.Slot,
		PublicIP: fmt.Sprintf("198.51.100.%d", f.next), State: provider.StateRunning, Created: f.Now(),
	}
	f.exits[e.ID] = e
	if s.Token != "" {
		f.tokens[s.Token] = e.ID
	}
	return e, nil
}

func (f *Fake) DeleteExit(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.exits[id]
	if !ok {
		return nil // deleting something already gone is success
	}
	e.State = provider.StateGone
	f.exits[id] = e
	return nil
}

func (f *Fake) ListExits(_ context.Context, env string) ([]provider.Exit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []provider.Exit
	for _, e := range f.exits {
		if e.Env == env {
			out = append(out, e)
		}
	}
	return out, nil
}
