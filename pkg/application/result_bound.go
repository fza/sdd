package application

import (
	"context"

	"github.com/networkteam/sdd/internal/model"
)

type resultBoundKey struct{}

// WithResultBound returns ctx carrying chars, how many characters one result
// of the call may take for the host it is served to (20261122-103723-d-tac-npg).
// A call whose context carries none is bounded for every host.
func WithResultBound(ctx context.Context, chars int) context.Context {
	return context.WithValue(ctx, resultBoundKey{}, model.ResultBound(chars))
}

// ResultBound is the bound WithResultBound set on ctx, in characters.
func ResultBound(ctx context.Context) int {
	return int(resultBound(ctx))
}

func resultBound(ctx context.Context) model.ResultBound {
	if ctx != nil {
		if bound, ok := ctx.Value(resultBoundKey{}).(model.ResultBound); ok {
			return bound
		}
	}
	return model.DefaultResultBound
}
