package application

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/internal/textpatch"
	"github.com/networkteam/sdd/pkg/application/types"
)

// StageItem is one file of a staging call. It takes exactly one source:
// Content stages the given bytes; Path stages the file read from that local
// path and keeps the path, so a playback confirm reads it again; Patches edits
// the file staged under Name before the call with ordered exact search-replace
// pairs.
type StageItem struct {
	Name    string
	Content []byte
	Path    string
	Patches []types.PatchPair
}

type stageWrite struct {
	name    string
	source  string
	content []byte
}

// StageAttachments stages items in call order, one result per item
// (20261114-121413-d-tac-n6r). Every item is checked before anything is
// written, and one failing item refuses the call with nothing staged. A write
// failing after the check stops the call: the files staged before it stay
// staged, and the error says which they are and whether the failing item was
// staged. A running capture taking its body from a staged handle gets the new
// body and hash.
func (w *WorkflowSession) StageAttachments(ctx context.Context, identity RequestIdentity, items []StageItem) ([]StagedFile, error) {
	w.setOperation(ctx, identity)
	writes, err := w.checkStageItems(ctx, identity, items)
	if err != nil {
		return nil, err
	}
	results := make([]StagedFile, 0, len(writes))
	for i, write := range writes {
		staged, done, err := w.stage(ctx, identity, write)
		if err != nil {
			return results, stageWriteError(i, write.name, done, results, err)
		}
		results = append(results, staged)
	}
	return results, nil
}

func (w *WorkflowSession) checkStageItems(ctx context.Context, identity RequestIdentity, items []StageItem) ([]stageWrite, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("files is empty: pass at least one file to stage")
	}
	seen := make(map[string]int, len(items))
	writes := make([]stageWrite, 0, len(items))
	for i, item := range items {
		first, duplicate := seen[item.Name]
		if duplicate {
			return nil, stageCheckError(i, item.Name, fmt.Errorf("item %d stages the same name; a name appears at most once per call", first+1))
		}
		seen[item.Name] = i
		write, err := w.checkStageItem(ctx, identity, item)
		if err != nil {
			return nil, stageCheckError(i, item.Name, err)
		}
		writes = append(writes, write)
	}
	return writes, nil
}

func (w *WorkflowSession) checkStageItem(ctx context.Context, identity RequestIdentity, item StageItem) (stageWrite, error) {
	err := validAttachmentName(item.Name)
	if err != nil {
		return stageWrite{}, err
	}
	sources := 0
	for _, present := range []bool{len(item.Content) > 0, item.Path != "", len(item.Patches) > 0} {
		if present {
			sources++
		}
	}
	if sources != 1 {
		return stageWrite{}, fmt.Errorf("pass exactly one of content, path, or patches")
	}
	write := stageWrite{name: item.Name, content: item.Content}
	switch {
	case item.Path != "":
		write.source, err = filepath.Abs(item.Path)
		if err != nil {
			return stageWrite{}, fmt.Errorf("path %s: %w", item.Path, err)
		}
		write.content, err = os.ReadFile(write.source)
		if err != nil {
			return stageWrite{}, fmt.Errorf("reading %s: %w", write.source, err)
		}
	case len(item.Patches) > 0:
		current, err := w.readStagedBlob(ctx, identity, item.Name)
		if err != nil {
			return stageWrite{}, err
		}
		patched, err := textpatch.Apply(string(current), item.Patches)
		if err != nil {
			return stageWrite{}, fmt.Errorf("editing the staged file: %w", err)
		}
		write.content = []byte(patched)
	}
	err = secrets.CheckFile(write.name, write.content)
	if err != nil {
		return stageWrite{}, err
	}
	err = w.checkRestagedBody(write.name)
	if err != nil {
		return stageWrite{}, err
	}
	return write, nil
}

func validAttachmentName(name string) error {
	if name == "" {
		return fmt.Errorf("name: filename is required")
	}
	if filepath.Base(name) != name || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("name: must be a plain filename")
	}
	return nil
}

func stageCheckError(index int, name string, err error) error {
	return fmt.Errorf("files item %d %s: %w (nothing was staged)", index+1, strconv.Quote(name), err)
}

func stageWriteError(index int, name string, staged bool, before []StagedFile, err error) error {
	state := "was not staged"
	if staged {
		state = "was staged"
	}
	kept := "no file was staged before it"
	if len(before) > 0 {
		handles := make([]string, 0, len(before))
		for _, file := range before {
			handles = append(handles, strconv.Quote(file.Handle))
		}
		kept = "staged before it and still staged: " + strings.Join(handles, ", ")
	}
	return fmt.Errorf("files item %d %s failed after the check, so the call stopped: %w; this item %s; %s", index+1, strconv.Quote(name), err, state, kept)
}

// stage writes one checked item. The unfinished-write check runs again here,
// since an earlier item's append may have caught up with another attach's
// log. done reports whether the item is staged even when err is set.
func (w *WorkflowSession) stage(ctx context.Context, identity RequestIdentity, write stageWrite) (staged StagedFile, done bool, err error) {
	err = w.checkRestagedBody(write.name)
	if err != nil {
		return StagedFile{}, false, err
	}
	blob, err := w.app.StageBlob(ctx, identity, w.project, SessionRef{Subject: w.binding.Subject, Session: w.binding.SessionID}, write.name, write.content)
	if err != nil {
		return StagedFile{}, false, err
	}
	item := map[string]string{"handle": write.name, "blob_id": blob.ID}
	if write.source != "" {
		item["source"] = write.source
	}
	payload, err := json.Marshal(item)
	if err != nil {
		return StagedFile{}, false, err
	}
	err = w.appendStoredEvent(workflowStagedBlobCode, payload)
	if err != nil {
		return StagedFile{}, false, err
	}
	w.staged[write.name] = blob.ID
	w.recordSource(write.name, write.source)
	bound, err := w.syncRestagedBody(write.name, write.content)
	if err != nil {
		return StagedFile{}, true, err
	}
	return StagedFile{Handle: write.name, SHA256: stagedSHA256(write.content), Bound: bound}, true, nil
}
