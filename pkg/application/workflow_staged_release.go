package application

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/networkteam/sdd/internal/engine"
)

// workflowStagedBlobReleasedCode records a staged attachment deleted once the
// entry using it was published (20261114-095140-d-cpt-ad4, rule 1): its handle
// no longer names a staged file.
const workflowStagedBlobReleasedCode = "workflow_staged_blob_released"

type stagedBlobRelease struct {
	Handle string `json:"handle"`
	BlobID string `json:"blob_id"`
}

func decodeStagedBlobRelease(stored StoredEvent) (stagedBlobRelease, error) {
	if !SupportedSessionCodecVersion(stored.CodecVersion) {
		return stagedBlobRelease{}, &ApplicationError{Code: ErrorMigrationRequired, Message: "unsupported staged blob event codec", Version: stored.CodecVersion}
	}
	var release stagedBlobRelease
	if err := json.Unmarshal(stored.Payload, &release); err != nil {
		return stagedBlobRelease{}, fmt.Errorf("decoding staged blob release: %w", err)
	}
	if release.Handle == "" || release.BlobID == "" {
		return stagedBlobRelease{}, fmt.Errorf("decoding staged blob release: handle and blob_id are required")
	}
	return release, nil
}

// releaseStaged drops a released handle from staged unless it was staged
// again since.
func releaseStaged(staged map[string]string, release stagedBlobRelease) bool {
	if staged[release.Handle] != release.BlobID {
		return false
	}
	delete(staged, release.Handle)
	return true
}

// releasePublished deletes the staged attachments the entry just published,
// keeping those another instance of the session still claims. It logs the
// release first, at the version whose state it computed the claims from, and
// deletes only once that append lands; an append another attach's write beat
// catches up and computes the claims again (20261114-120345-d-tac-6n4, 1).
func (w *WorkflowSession) releasePublished(instance string, attachments []StagedAttachment) error {
	released, err := w.logRelease(instance, attachments)
	if isSessionConflict(err) {
		err = w.catchUp(nil)
		if err == nil {
			released, err = w.logRelease(instance, attachments)
		}
	}
	if isSessionConflict(err) {
		return fmt.Errorf("%w; %s", err, reorientSuffix)
	}
	if err != nil {
		return err
	}
	ref := SessionRef{Subject: w.binding.Subject, Session: w.binding.SessionID}
	for _, release := range released {
		if releaseStaged(w.staged, release) {
			w.recordSource(release.Handle, "")
		}
		if err := w.app.blobs.DeleteBlob(w.ctx, ref, release.BlobID); err != nil {
			return err
		}
	}
	return nil
}

// logRelease appends the release of the attachments no other instance claims
// in the state the holder has, and returns what it released.
func (w *WorkflowSession) logRelease(instance string, attachments []StagedAttachment) ([]stagedBlobRelease, error) {
	claims, err := w.pendingClaims(instance)
	if err != nil {
		return nil, err
	}
	for _, inst := range w.session.Instances() {
		if inst.ID == instance || inst.Status != engine.StatusRunning || inst.Store.Has("entryId") {
			continue
		}
		claimHandles(claims, inst, w.staged)
	}
	var events []StoredEvent
	var released []stagedBlobRelease
	for _, attachment := range attachments {
		if claims[attachment.BlobID] {
			continue
		}
		release := stagedBlobRelease{Handle: attachment.Filename, BlobID: attachment.BlobID}
		payload, err := json.Marshal(release)
		if err != nil {
			return nil, err
		}
		events = append(events, StoredEvent{CodecVersion: SessionCodecVersion, Code: workflowStagedBlobReleasedCode, Payload: payload})
		released = append(released, release)
	}
	if len(events) == 0 {
		return nil, nil
	}
	return released, w.appendStoredEventsOnce(events)
}

// pendingClaims is the staged files the session's pending write needs, unless
// that write is skip's.
func (w *WorkflowSession) pendingClaims(skip string) (map[string]bool, error) {
	claims := map[string]bool{}
	pending := w.session.PendingMutation()
	if pending == nil || pending.Instance == skip {
		return claims, nil
	}
	staged, err := w.stagedAt(pending.Ref)
	if err != nil {
		return nil, err
	}
	if inst, ok := w.session.Instance(pending.Instance); ok {
		claimHandles(claims, inst, staged)
	}
	return claims, nil
}

func claimHandles(claims map[string]bool, inst *engine.Instance, staged map[string]string) {
	handles := workflowStoreStrings(inst.Store, "attachments")
	if body, ok := workflowStoreString(inst.Store, fieldBodyHandle); ok {
		handles = append(handles, body)
	}
	for _, handle := range handles {
		if blob, ok := staged[handle]; ok {
			claims[blob] = true
		}
	}
}

// freeEndedSession deletes an ended session's staged files but those claims
// names (20261114-095140-d-cpt-ad4, rule 1).
func (a *Application) freeEndedSession(ctx context.Context, ref SessionRef, events []StoredEvent, claims map[string]bool) error {
	if len(claims) == 0 {
		return a.blobs.DeleteStaged(ctx, ref)
	}
	for _, stored := range events {
		if stored.Code != workflowStagedBlobCode {
			continue
		}
		var item struct {
			BlobID string `json:"blob_id"`
		}
		if err := json.Unmarshal(stored.Payload, &item); err != nil {
			return fmt.Errorf("decoding staged blob event: %w", err)
		}
		if claims[item.BlobID] {
			continue
		}
		if err := a.blobs.DeleteBlob(ctx, ref, item.BlobID); err != nil {
			return err
		}
	}
	return nil
}
