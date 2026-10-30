package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

// A capture whose body comes from a staged file names it in bodyHandle; sdd
// keeps body and bodyHash in step with that file, so the playback
// confirmation, bound to the state snapshot, covers the file's bytes
// (20261030-120506-d-tac-l2b, 20261030-121523-d-tac-isy).
const (
	fieldBodyHandle = "bodyHandle"
	fieldBodyHash   = "bodyHash"
	fieldBody       = "body"
	fieldBodyPatch  = "bodyPatch"
)

// StagedFile is a staged file's handle and the sha256 of its raw bytes.
type StagedFile struct {
	Handle string
	SHA256 string
	// Bound names the running captures whose body the staging replaced.
	Bound []string
}

func stagedSHA256(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// takesStagedBody reports whether the instance's procedure declares a body
// taken from a staged file.
func takesStagedBody(inst *engine.Instance) bool {
	_, ok := inst.Spec.State[fieldBodyHandle]
	return ok
}

// stagedBodyFields resolves the staged body a report names or keeps bound. It
// refuses what a report may not carry beside a staged body and a header
// stating a reported field otherwise, and returns body, bodyHash and
// bodyHandle to write when the report names a handle; nil when nothing is to
// be written.
func (w *WorkflowSession) stagedBodyFields(ctx context.Context, identity RequestIdentity, inst *engine.Instance, fields map[string]any) (map[string]any, error) {
	if !takesStagedBody(inst) {
		return nil, nil
	}
	if _, reported := fields[fieldBodyHash]; reported {
		return nil, fmt.Errorf("field %q is set by sdd from %q and cannot be reported", fieldBodyHash, fieldBodyHandle)
	}
	named, reportsHandle := fields[fieldBodyHandle]
	if reportsHandle && named == nil {
		return map[string]any{fieldBodyHandle: nil, fieldBodyHash: nil}, nil
	}
	handle, _ := named.(string)
	if !reportsHandle {
		bound, _ := inst.Store.Get(fieldBodyHandle)
		handle, _ = bound.(string)
	}
	if handle == "" {
		return nil, nil
	}
	for _, inline := range []string{fieldBody, fieldBodyPatch} {
		if _, ok := fields[inline]; !ok {
			continue
		}
		if reportsHandle {
			return nil, fmt.Errorf("report %q or %q, not both: the body comes from the staged file %q", fieldBodyHandle, inline, handle)
		}
		return nil, fmt.Errorf("field %q cannot change a body taken from the staged file %q: patch the staged file with stage_attachment, or report %q null to draft the body inline", inline, handle, fieldBodyHandle)
	}
	content, err := w.readStagedBlob(ctx, identity, handle)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", fieldBodyHandle, err)
	}
	header, body := model.SplitStagedBody(string(content))
	if err := stagedHeaderAgrees(header, handle, inst, fields); err != nil {
		return nil, err
	}
	if !reportsHandle {
		return nil, nil
	}
	return map[string]any{fieldBodyHandle: handle, fieldBody: body, fieldBodyHash: stagedSHA256(content)}, nil
}

// stagedHeaderAgrees refuses a header key stating a report field otherwise
// than the capture does once fields land.
func stagedHeaderAgrees(header map[string]any, handle string, inst *engine.Instance, fields map[string]any) error {
	if header == nil {
		return nil
	}
	reported := make(map[string]any, len(model.StagedHeaderKeys))
	for _, key := range model.StagedHeaderKeys {
		if value, ok := fields[key]; ok {
			if value != nil {
				reported[key] = value
			}
			continue
		}
		if value, ok := inst.Store.Get(key); ok {
			reported[key] = value
		}
	}
	if key, mismatch := model.StagedHeaderMismatch(header, reported); mismatch {
		return fmt.Errorf("the header of the staged file %q states %q otherwise than the report; make them agree", handle, key)
	}
	return nil
}

// applyStagedBody merges the staged body a report or chooser answer names
// into the fields it carries, so the engine validates and writes them in one
// step and a refused answer leaves nothing behind.
func (w *WorkflowSession) applyStagedBody(ctx context.Context, identity RequestIdentity, request WorkflowAdvanceRequest, answer bool) error {
	inst, ok := w.session.Instance(request.Instance)
	if !ok || inst.Status != engine.StatusRunning {
		return nil
	}
	fields := request.Report
	if answer {
		fields, _ = request.Report["fields"].(map[string]any)
	}
	resolved, err := w.stagedBodyFields(ctx, identity, inst, fields)
	if err != nil || resolved == nil {
		return err
	}
	for name, value := range resolved {
		fields[name] = value
	}
	return nil
}

// checkRestagedBody refuses a staging whose header contradicts a running
// capture taking its body from the handle, before anything is staged.
func (w *WorkflowSession) checkRestagedBody(handle string, content []byte) error {
	bound := w.boundCaptures(handle)
	if len(bound) == 0 {
		return nil
	}
	if err := w.session.SinkErr(); err != nil {
		return err
	}
	if pending := w.session.PendingMutation(); pending != nil {
		return fmt.Errorf("capture %s takes its body from %q, and the operation %q in this session is unfinished: continue or cancel it before staging that file again", bound[0].ID, handle, pending.Command)
	}
	header, _ := model.SplitStagedBody(string(content))
	for _, inst := range bound {
		if err := stagedHeaderAgrees(header, handle, inst, nil); err != nil {
			return fmt.Errorf("capture %s: %w", inst.ID, err)
		}
	}
	return nil
}

// syncRestagedBody writes a re-staged file's body and hash into every running
// capture taking its body from the handle, so a playback confirmed before
// the staging no longer holds.
func (w *WorkflowSession) syncRestagedBody(handle string, content []byte) ([]string, error) {
	_, body := model.SplitStagedBody(string(content))
	hash := stagedSHA256(content)
	var synced []string
	for _, inst := range w.boundCaptures(handle) {
		current, _ := inst.Store.Get(fieldBodyHash)
		if current == hash {
			continue
		}
		if err := w.session.Update(inst.ID, map[string]any{fieldBody: body, fieldBodyHash: hash}); err != nil {
			return synced, fmt.Errorf("capture %s: %w", inst.ID, err)
		}
		if err := w.session.SinkErr(); err != nil {
			return synced, err
		}
		synced = append(synced, inst.ID)
	}
	return synced, nil
}

func (w *WorkflowSession) boundCaptures(handle string) []*engine.Instance {
	var bound []*engine.Instance
	for _, inst := range w.session.Instances() {
		// A capture holding an entryId has written its entry; its body is
		// final.
		if inst.Status != engine.StatusRunning || !takesStagedBody(inst) || inst.Store.Has("entryId") {
			continue
		}
		if named, _ := inst.Store.Get(fieldBodyHandle); named == handle {
			bound = append(bound, inst)
		}
	}
	return bound
}
