package reconcile

import (
	"context"
	"errors"
	"fmt"

	"niblr/internal/provider"
)

// Template carries the parts of an ExitSpec that are the same for every slot.
type Template struct {
	Env       string
	Region    string
	Size      string
	CloudInit string
}

// Apply executes a plan through any provider.
//
// Deletes run first so a scale-down or stray cleanup frees quota before creates need it. A failure on
// one action does not stop the others (one bad slot must not leave the rest of the pool unrepaired);
// all errors are joined and returned. Creates carry an idempotency token derived from round+slot, so
// retrying the same round after a timeout cannot produce a second exit.
func Apply(ctx context.Context, p provider.Provider, t Template, round string, a Actions) ([]provider.Exit, error) {
	var errs []error
	for _, id := range a.DeleteIDs {
		if err := p.DeleteExit(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("delete %s: %w", id, err))
		}
	}
	var created []provider.Exit
	for _, slot := range a.CreateSlots {
		e, err := p.CreateExit(ctx, provider.ExitSpec{
			Env: t.Env, Slot: slot, Token: fmt.Sprintf("%s/%s/%d", t.Env, round, slot),
			Region: t.Region, Size: t.Size, CloudInit: t.CloudInit,
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("create slot %d: %w", slot, err))
			continue
		}
		created = append(created, e)
	}
	return created, errors.Join(errs...)
}

// Converge runs one full reconcile round: list by tag, plan, apply.
func Converge(ctx context.Context, p provider.Provider, t Template, want int, round string) (Actions, []provider.Exit, error) {
	found, err := p.ListExits(ctx, t.Env)
	if err != nil {
		return Actions{}, nil, fmt.Errorf("list exits: %w", err)
	}
	a := Plan(want, found)
	created, err := Apply(ctx, p, t, round, a)
	return a, created, err
}
