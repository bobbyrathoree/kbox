package release

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"

	"k8s.io/client-go/kubernetes"

	"github.com/bobbyrathoree/kbox/internal/apply"
	"github.com/bobbyrathoree/kbox/internal/render"
)

// RollbackOptions configures rollback behavior
type RollbackOptions struct {
	// ToRevision specifies a specific revision to rollback to (0 = previous)
	ToRevision int
	// DryRun shows what would happen without making changes
	DryRun bool
	// Output for status messages
	Output io.Writer
}

// Rollback reverts to a previous release
func Rollback(ctx context.Context, client *kubernetes.Clientset, namespace, appName string, opts RollbackOptions) (*RollbackResult, error) {
	store := NewStore(client, namespace, appName)

	// Determine target release
	var target *Release
	var err error

	if opts.ToRevision > 0 {
		target, err = store.Get(ctx, opts.ToRevision)
		if err != nil {
			return nil, fmt.Errorf("cannot rollback to revision %d: %w", opts.ToRevision, err)
		}
	} else {
		target, err = store.GetPrevious(ctx)
		if err != nil {
			return nil, fmt.Errorf("cannot rollback: %w", err)
		}
	}

	// Get the config from the target release
	cfg, err := target.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load release config: %w", err)
	}

	result := &RollbackResult{
		FromRevision: 0, // Will be set below
		ToRevision:   target.Revision,
		Image:        target.Image,
	}

	// Get current revision for reporting
	current, err := store.GetLatest(ctx)
	if err == nil {
		result.FromRevision = current.Revision
	}

	if opts.DryRun {
		return result, nil
	}

	// Re-render the bundle from the stored config
	renderer := render.New(cfg)
	bundle, err := renderer.Render()
	if err != nil {
		return nil, fmt.Errorf("failed to render release: %w", err)
	}

	// Apply the bundle
	out := opts.Output
	if out == nil {
		out = ioutil.Discard
	}
	engine := apply.NewEngine(client, out)
	_, err = engine.Apply(ctx, bundle)
	if err != nil {
		return nil, fmt.Errorf("failed to apply rollback: %w", err)
	}

	// Wait for rollout
	if err := engine.WaitForRollout(ctx, namespace, appName); err != nil {
		return nil, fmt.Errorf("rollback applied but rollout failed: %w", err)
	}

	// Save the rollback as a new release (so we can rollback the rollback)
	newRevision, err := store.Save(ctx, cfg)
	if err != nil {
		// Non-fatal - the rollback succeeded, just history tracking failed
		if opts.Output != nil {
			fmt.Fprintf(opts.Output, "Warning: failed to save rollback to history: %v\n", err)
		}
	} else {
		result.NewRevision = newRevision
	}

	return result, nil
}

// RollbackResult contains information about a completed rollback
type RollbackResult struct {
	FromRevision int
	ToRevision   int
	NewRevision  int    // The new release created by the rollback
	Image        string // The image we rolled back to
}

// String returns a human-readable summary
func (r *RollbackResult) String() string {
	if r.NewRevision > 0 {
		return fmt.Sprintf("Rolled back from %s to %s (saved as %s)",
			FormatRevision(r.FromRevision),
			FormatRevision(r.ToRevision),
			FormatRevision(r.NewRevision))
	}
	return fmt.Sprintf("Rolled back from %s to %s",
		FormatRevision(r.FromRevision),
		FormatRevision(r.ToRevision))
}
