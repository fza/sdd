package handlers

import (
	"context"
	"errors"
	"fmt"

	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/repos"
)

// UpstreamLocator names the remote a repository's network operations reach:
// the gate limits by its URL.
type UpstreamLocator interface {
	UpstreamURL(ctx context.Context) (string, error)
}

// gated runs op as one operation of kind against the remote remote names,
// when the git gate admits it (20261107-095427-d-tac-yux). A deferral
// returns *gitgate.DeferredError without running op.
func (h *Handler) gated(ctx context.Context, kind gitgate.Kind, remote UpstreamLocator, op func(ctx context.Context) error) error {
	if h.gate == nil {
		return repos.ErrNoGate
	}
	url, err := remote.UpstreamURL(ctx)
	if err != nil {
		return fmt.Errorf("resolving the upstream URL: %w", err)
	}
	target, err := gitgate.TargetFromURL(url)
	if err != nil {
		return err
	}
	return h.gate.Run(ctx, target, kind, op)
}

func deferral(err error) (*gitgate.DeferredError, bool) {
	var deferred *gitgate.DeferredError
	ok := errors.As(err, &deferred)
	return deferred, ok
}
