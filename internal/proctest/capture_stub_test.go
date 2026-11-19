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

// failingRelevanceStore fails the first Append once armed, as a ledger that
// goes away between the publish and its verdicts does.
type failingRelevanceStore struct {
	sdd.RelevanceStore
	armed bool
}

func (s *failingRelevanceStore) Append(ctx context.Context, segment []byte) error {
	if s.armed {
		s.armed = false
		return errors.New("relevance ledger unavailable")
	}
	return s.RelevanceStore.Append(ctx, segment)
}
