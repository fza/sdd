package embed

import (
	"context"
	"slices"

	pkgembed "github.com/networkteam/sdd/pkg/llm/embed"
)

// Prefetch starts embedding req at once and returns an Embedder that answers
// that same request from the result, so the provider call runs while the
// caller does work it does not depend on. Any other request goes to next.
func Prefetch(ctx context.Context, next pkgembed.Embedder, req pkgembed.Request) pkgembed.Embedder {
	p := &prefetched{Embedder: next, req: req, done: make(chan struct{})}
	go func() {
		defer close(p.done)
		p.result, p.err = next.Embed(ctx, req)
	}()
	return p
}

type prefetched struct {
	pkgembed.Embedder
	req    pkgembed.Request
	done   chan struct{}
	result pkgembed.Result
	err    error
}

func (p *prefetched) Embed(ctx context.Context, req pkgembed.Request) (pkgembed.Result, error) {
	if req.Purpose != p.req.Purpose || !slices.Equal(req.Texts, p.req.Texts) {
		return p.Embedder.Embed(ctx, req)
	}
	select {
	case <-p.done:
		return p.result, p.err
	case <-ctx.Done():
		return pkgembed.Result{}, ctx.Err()
	}
}
