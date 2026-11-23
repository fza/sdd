package application

import (
	"context"
	"fmt"
)

// ReminderInstructions composes the short reminder a host serves in place of
// instructions it served earlier, keeping the gate diagnostics. The reminder
// names resume_session as the way back to the full text. Hosts that serve by
// pieces compose with ComposeInstructions instead.
func (s *WorkflowServe) ReminderInstructions() string {
	if s == nil {
		return ""
	}
	reminder := fmt.Sprintf("(step %s instructions were served earlier this session — follow them; goal: %s. Lost them to a context compaction? resume_session with this session's handle re-serves this position in full.)", s.Step, s.Goal)
	return s.ComposeInstructions(reminder)
}

// ServedBefore reports no hash as served: the session tracks served text by
// pieces and the text question (RecordPieces), so a host asking by hash
// serves every block in full.
func (w *WorkflowSession) ServedBefore(_ string) bool {
	return false
}

// RecordServed logs the call's pieces as RecordPieces does; the hashes are
// not kept (ServedBefore).
func (w *WorkflowSession) RecordServed(ctx context.Context, identity RequestIdentity, _ []string) error {
	return w.RecordPieces(ctx, identity)
}
