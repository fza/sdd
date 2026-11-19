package proctest_test

import (
	"context"
	"errors"

	sdd "github.com/networkteam/sdd/pkg/application"
)

type captureCompletionFailure struct{ calls int }

func (*captureCompletionFailure) Name() string { return "capture-completion" }
func (f *captureCompletionFailure) Finalize(context.Context, sdd.AppliedMutation) error {
	f.calls++
	if f.calls == 1 {
		return errors.New("completion unavailable after publication")
	}
	return nil
}

// failingRelevanceStore fails the failAt-th Append once, counted from when
// failAt is set, as a ledger that goes away behind the publish does.
type failingRelevanceStore struct {
	sdd.RelevanceStore
	failAt int
}

func (s *failingRelevanceStore) Append(ctx context.Context, segment []byte) error {
	if s.failAt > 0 {
		s.failAt--
		if s.failAt == 0 {
			return errors.New("relevance ledger unavailable")
		}
	}
	return s.RelevanceStore.Append(ctx, segment)
}

// captureCompletionCount counts the publish's completions.
type captureCompletionCount struct{ calls int }

func (*captureCompletionCount) Name() string { return "capture-completion" }
func (f *captureCompletionCount) Finalize(context.Context, sdd.AppliedMutation) error {
	f.calls++
	return nil
}
