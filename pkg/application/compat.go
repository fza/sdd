package application

import (
	"context"
	"fmt"

	"github.com/networkteam/sdd/pkg/application/types"
)

// ReminderInstructions composes the short reminder used when a host has
// already served this instruction unit, while retaining any gate diagnostics.
// The reminder names resume_session as the way back to the full text.
// Hosts that dedup per piece use WorkflowSession.HoldsEntry and
// ComposeInstructions instead.
func (s *WorkflowServe) ReminderInstructions() string {
	if s == nil {
		return ""
	}
	reminder := fmt.Sprintf("(step %s instructions were served earlier this session — follow them; goal: %s. Lost them to a context compaction? resume_session with this session's handle re-serves this position in full.)", s.Step, s.Goal)
	return s.ComposeInstructions(reminder)
}

// ServedBefore always reports false: the session tracks served text by
// anchored pieces, not by content hash, so a host asking by hash serves every
// block in full. HoldsEntry and Unproved answer per piece.
func (w *WorkflowSession) ServedBefore(_ string) bool {
	return false
}

// RecordServed records what the call served. The hashes are not kept, see
// ServedBefore; it logs the call's anchors as RecordAnchors does.
func (w *WorkflowSession) RecordServed(ctx context.Context, identity RequestIdentity, _ []string) error {
	return w.RecordAnchors(ctx, identity)
}

// StageAttachment stages content under filename and returns its handle.
// StageAttachments stages several files in one call and reports each file's
// hash.
func (w *WorkflowSession) StageAttachment(ctx context.Context, identity RequestIdentity, filename string, content []byte) (string, error) {
	staged, err := w.StageAttachments(ctx, identity, []StageItem{{Name: filename, Content: content}})
	if err != nil {
		return "", err
	}
	return staged[0].Handle, nil
}

// EditStagedAttachment applies ordered exact search-replace pairs to the file
// staged under handle, as a StageAttachments item with Patches does.
func (w *WorkflowSession) EditStagedAttachment(ctx context.Context, identity RequestIdentity, handle string, pairs []types.PatchPair) error {
	_, err := w.StageAttachments(ctx, identity, []StageItem{{Name: handle, Patches: pairs}})
	return err
}
