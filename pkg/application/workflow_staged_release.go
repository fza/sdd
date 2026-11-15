package application

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/networkteam/sdd/internal/engine"
)

// workflowStagedBlobReleasedCode records a staged attachment deleted once the
// entry using it was published: its handle no longer names a staged file.
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

// publishedFiles is the staged files an entry was written from: its
// attachments and the file its body came from.
func publishedFiles(store *engine.Store, staged map[string]string, attachments []StagedAttachment) []StagedAttachment {
	handle, ok := workflowStoreString(store, fieldBodyHandle)
	if !ok {
		return attachments
	}
	blob, ok := staged[handle]
	if !ok || slices.ContainsFunc(attachments, func(a StagedAttachment) bool { return a.BlobID == blob }) {
		return attachments
	}
	return append(slices.Clone(attachments), StagedAttachment{Filename: handle, BlobID: blob})
}

// releasePublished deletes the staged files the entry just published, keeping
// those another running capture of the session still names. It logs the
// release first and deletes only once that append lands: an append another
// attach's write beat is refused as a session conflict, since the claims were
// computed without that write, and every file stays.
func (w *WorkflowSession) releasePublished(instance string, attachments []StagedAttachment) error {
	claims := map[string]bool{}
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
			return err
		}
		events = append(events, StoredEvent{CodecVersion: SessionCodecVersion, Code: workflowStagedBlobReleasedCode, Payload: payload})
		released = append(released, release)
	}
	if len(events) == 0 {
		return nil
	}
	if err := w.appendStoredEvents(events); err != nil {
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

// freeIfEnded deletes the staged files of a session the operation just ended.
// It runs once the operation returns, never inside the append that ended it,
// so a failed delete cannot read as a failed append. No session ends with a
// write pending, so nothing staged is still claimed.
func (w *WorkflowSession) freeIfEnded(ctx context.Context) error {
	if !w.endedUnfreed {
		return nil
	}
	w.endedUnfreed = false
	if err := w.app.blobs.DeleteStaged(ctx, SessionRef{Subject: w.binding.Subject, Session: w.ID()}); err != nil {
		return fmt.Errorf("session %s ended; freeing its staged files: %w", w.ID(), err)
	}
	return nil
}
