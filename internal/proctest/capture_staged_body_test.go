package proctest_test

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/application/types"
)

const stagedBodyFile = "---\nentryKind: gap\nlayer: tac\nkind: ignored-key\ntitle: anything\n---\nA tactical gap drafted in a reviewed file.\n"

func sha256Hex(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// stagedBodyDraft is captureDraft with its body taken from a staged handle.
func stagedBodyDraft(handle string) map[string]any {
	draft := captureDraft()
	delete(draft, "body")
	draft["bodyHandle"] = handle
	return draft
}

func TestCapture_BodyFromStagedFile(t *testing.T) {
	world, session := newCaptureWorld(t, "staged-body")
	staged := session.StageFile(t, "draft.md", []byte(stagedBodyFile))
	if staged.SHA256 != sha256Hex(stagedBodyFile) {
		t.Fatalf("staging should answer the sha256 of the raw bytes, got %q", staged.SHA256)
	}
	serve := session.Start(t, "capture", nil)
	serve = session.Report(t, serve.Instance, stagedBodyDraft(staged.Handle))
	proctest.RequireStep(t, serve, "playback")
	if !strings.Contains(serve.Instructions, staged.SHA256) {
		t.Errorf("playback should show the staged body's hash:\n%s", serve.Instructions)
	}
	for _, option := range serve.PendingChooser.Options {
		if option.Choice == "adjust" && !slices.Contains(option.Collect, "bodyHandle?") {
			t.Errorf("an adjust should offer to name another staged body, got %v", option.Collect)
		}
	}

	serve = session.Answer(t, serve.Instance, "playback", "confirm", nil, "capture it")
	proctest.RequireStep(t, serve, "verifySummary")
	serve = session.Answer(t, serve.Instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	proctest.RequireStatus(t, serve, "completed")
	entryID, _ := serve.Produced["entryId"].(string)
	entry := proctest.LoadEntry(t, world.GraphDir, entryID)
	if !strings.Contains(entry.Content, "A tactical gap drafted in a reviewed file.") {
		t.Errorf("the entry body should be the staged text after its header, got %q", entry.Content)
	}
	if strings.Contains(entry.Content, "ignored-key") {
		t.Errorf("the header should stay out of the entry body, got %q", entry.Content)
	}
}

func TestCapture_StagedBodyRefusals(t *testing.T) {
	tests := []struct {
		name   string
		file   string
		report func(handle string) map[string]any
		want   string
	}{
		{
			name: "a body beside the handle",
			file: stagedBodyFile,
			report: func(handle string) map[string]any {
				draft := stagedBodyDraft(handle)
				draft["body"] = "inline"
				return draft
			},
			want: "not both",
		},
		{
			name: "a body patch beside the handle",
			file: stagedBodyFile,
			report: func(handle string) map[string]any {
				draft := stagedBodyDraft(handle)
				draft["bodyPatch"] = []any{map[string]any{"old": "gap", "new": "x"}}
				return draft
			},
			want: "not both",
		},
		{
			name:   "a handle naming no staged file",
			file:   stagedBodyFile,
			report: func(string) map[string]any { return stagedBodyDraft("missing.md") },
			want:   `"missing.md"`,
		},
		{
			name: "a reported hash",
			file: stagedBodyFile,
			report: func(handle string) map[string]any {
				draft := stagedBodyDraft(handle)
				draft["bodyHash"] = "abc"
				return draft
			},
			want: "cannot be reported",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, session := newCaptureWorld(t, "staged-body-refusal")
			handle := session.Stage(t, "draft.md", []byte(tt.file))
			serve := session.Start(t, "capture", nil)
			_, err := session.ReportErr(t, serve.Instance, tt.report(handle))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("the report should be refused naming what is wrong, got %v", err)
			}
		})
	}
}

func TestCapture_BoundStagedBodyRefusesInlineEdits(t *testing.T) {
	_, session := newCaptureWorld(t, "staged-body-bound")
	handle := session.Stage(t, "draft.md", []byte(stagedBodyFile))
	serve := session.Start(t, "capture", nil)
	serve = session.Report(t, serve.Instance, stagedBodyDraft(handle))
	proctest.RequireStep(t, serve, "playback")

	_, err := session.AnswerErr(t, serve.Instance, "playback", "adjust", map[string]any{
		"bodyPatch": []any{map[string]any{"old": "gap", "new": "repetition"}},
	}, "fix the wording")
	if err == nil || !strings.Contains(err.Error(), "patch the staged file") {
		t.Fatalf("a body patch on a staged body should be refused, got %v", err)
	}

	serve = session.Answer(t, serve.Instance, "playback", "adjust", map[string]any{"bodyHandle": nil, "body": "Drafted inline instead."}, "draft it here")
	proctest.RequireStep(t, serve, "playback")
	if !strings.Contains(serve.Instructions, "Drafted inline instead.") {
		t.Errorf("clearing the handle should let the body be drafted inline:\n%s", serve.Instructions)
	}
	serve = session.Answer(t, serve.Instance, "playback", "adjust", map[string]any{
		"bodyPatch": []any{map[string]any{"old": "inline instead", "new": "inline after all"}},
	}, "fix the wording")
	if !strings.Contains(serve.Instructions, "Drafted inline after all.") {
		t.Errorf("a body drafted inline should take body patches again:\n%s", serve.Instructions)
	}
}

func TestCapture_RestagedBodyReopensConfirmedPlayback(t *testing.T) {
	tests := []struct {
		name     string
		restage  func(t *testing.T, session *proctest.Session, handle string)
		wantBody string
	}{
		{
			name: "staged again whole",
			restage: func(t *testing.T, session *proctest.Session, handle string) {
				session.Stage(t, handle, []byte(strings.Replace(stagedBodyFile, "reviewed file", "revised file", 1)))
			},
			wantBody: "A tactical gap drafted in a revised file.",
		},
		{
			name: "patched in staging",
			restage: func(t *testing.T, session *proctest.Session, handle string) {
				if _, err := session.WF.EditStagedAttachment(t.Context(), session.World.Identity, handle, []types.PatchPair{{Old: "reviewed file", New: "patched file"}}); err != nil {
					t.Fatal(err)
				}
			},
			wantBody: "A tactical gap drafted in a patched file.",
		},
		{
			name: "only the header changed",
			restage: func(t *testing.T, session *proctest.Session, handle string) {
				session.Stage(t, handle, []byte(strings.Replace(stagedBodyFile, "title: anything", "title: something else", 1)))
			},
			wantBody: "A tactical gap drafted in a reviewed file.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			world, session := newCaptureWorld(t, "staged-body-restage")
			world.LLM.PreflightFindings = []proctest.PreflightFinding{{Severity: "high", Category: "test-block", Observation: "Review this finding."}}
			handle := session.Stage(t, "draft.md", []byte(stagedBodyFile))
			serve := session.Start(t, "capture", nil)
			instance := serve.Instance
			proctest.RequireStep(t, session.Report(t, instance, stagedBodyDraft(handle)), "playback")
			serve = session.Answer(t, instance, "playback", "confirm", nil, "capture it")
			proctest.RequireStep(t, serve, "reviseOrOverride")

			tt.restage(t, session, handle)
			world.LLM.PreflightFindings = nil
			serve = session.Answer(t, instance, "reviseOrOverride", "override", nil, "the finding does not apply")
			proctest.RequireStep(t, serve, "playback")
			if strings.Contains(serve.Instructions, "Draft unchanged") {
				t.Errorf("the reopened playback should show what the staging changed:\n%s", serve.Instructions)
			}

			serve = session.Answer(t, instance, "playback", "confirm", nil, "capture the revision")
			proctest.RequireStep(t, serve, "verifySummary")
			serve = session.Answer(t, instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
			entryID, _ := serve.Produced["entryId"].(string)
			if entry := proctest.LoadEntry(t, world.GraphDir, entryID); !strings.Contains(entry.Content, tt.wantBody) {
				t.Errorf("the entry should carry the body confirmed last, got %q", entry.Content)
			}
		})
	}
}

func TestCapture_HeaderDisagreeingAtTheConfirmIsRefused(t *testing.T) {
	_, session := newCaptureWorld(t, "staged-body-header-confirm")
	handle := session.Stage(t, "draft.md", []byte("---\nconfidence: high\n---\nA tactical gap with a header.\n"))
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	proctest.RequireStep(t, session.Report(t, instance, stagedBodyDraft(handle)), "playback")

	_, err := session.AnswerErr(t, instance, "playback", "confirm", nil, "capture it")
	if err == nil || !strings.Contains(err.Error(), `"confidence"`) {
		t.Fatalf("a confirm while header and fields disagree should be refused naming the key, got %v", err)
	}
	position, err := session.WF.ServeAll(t.Context(), session.World.Identity)
	if err != nil {
		t.Fatal(err)
	}
	for _, open := range position.Open {
		if open.Instance == instance && open.Step != "playback" {
			t.Errorf("a refused confirm should leave the capture at playback, got %q", open.Step)
		}
	}
}

// answerPlayback answers playback, once more when the answer met a re-staged
// body not yet served and was turned into a fresh playback instead.
func answerPlayback(t *testing.T, session *proctest.Session, instance, choice string, fields map[string]any) *sdd.WorkflowServe {
	t.Helper()
	serve := session.Answer(t, instance, "playback", choice, fields, "as discussed")
	if hasDiagnostic(serve, "not answered") {
		serve = session.Answer(t, instance, "playback", choice, fields, "as discussed")
	}
	return serve
}

func TestCapture_HeaderFieldEditInEitherOrder(t *testing.T) {
	const restaged = "---\nentryKind: gap\nlayer: strategic\n---\nA strategic gap after the edit.\n"
	edits := map[string]func(t *testing.T, session *proctest.Session, instance, handle string){
		"re-stage first": func(t *testing.T, session *proctest.Session, instance, handle string) {
			session.Stage(t, handle, []byte(restaged))
			proctest.RequireStep(t, answerPlayback(t, session, instance, "adjust", map[string]any{"layer": "strategic"}), "playback")
		},
		"report first": func(t *testing.T, session *proctest.Session, instance, handle string) {
			proctest.RequireStep(t, answerPlayback(t, session, instance, "adjust", map[string]any{"layer": "strategic"}), "playback")
			session.Stage(t, handle, []byte(restaged))
		},
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			world, session := newCaptureWorld(t, "staged-body-header-edit")
			handle := session.Stage(t, "draft.md", []byte(stagedBodyFile))
			serve := session.Start(t, "capture", nil)
			instance := serve.Instance
			proctest.RequireStep(t, session.Report(t, instance, stagedBodyDraft(handle)), "playback")

			edit(t, session, instance, handle)

			serve = answerPlayback(t, session, instance, "confirm", nil)
			proctest.RequireStep(t, serve, "verifySummary")
			serve = session.Answer(t, instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
			entryID, _ := serve.Produced["entryId"].(string)
			if !strings.Contains(entryID, "-stg-") {
				t.Errorf("the entry should carry the edited layer, got %q", entryID)
			}
			if entry := proctest.LoadEntry(t, world.GraphDir, entryID); !strings.Contains(entry.Content, "A strategic gap after the edit.") {
				t.Errorf("the entry should carry the re-staged body, got %q", entry.Content)
			}
		})
	}
}

func TestCapture_AdjustNamesAnotherStagedBody(t *testing.T) {
	world, session := newCaptureWorld(t, "staged-body-switch")
	handle := session.Stage(t, "draft.md", []byte(stagedBodyFile))
	other := session.StageFile(t, "second.md", []byte("A second draft of the gap.\n"))
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	proctest.RequireStep(t, session.Report(t, instance, stagedBodyDraft(handle)), "playback")

	serve = session.Answer(t, instance, "playback", "adjust", map[string]any{"bodyHandle": other.Handle}, "use the second draft")
	proctest.RequireStep(t, serve, "playback")
	if !strings.Contains(serve.Instructions, other.SHA256) {
		t.Errorf("playback should show the hash of the newly named file:\n%s", serve.Instructions)
	}
	proctest.RequireStep(t, session.Answer(t, instance, "playback", "confirm", nil, "capture it"), "verifySummary")
	serve = session.Answer(t, instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	entryID, _ := serve.Produced["entryId"].(string)
	if entry := proctest.LoadEntry(t, world.GraphDir, entryID); !strings.Contains(entry.Content, "A second draft of the gap.") {
		t.Errorf("the entry should carry the body of the file named last, got %q", entry.Content)
	}
}

const unseenStagedBody = "A tactical gap in text not yet played back.\n"

func TestCapture_RestageAtPlaybackServesTheChangeBeforeAConfirm(t *testing.T) {
	world, session := newCaptureWorld(t, "staged-body-unseen")
	handle := session.Stage(t, "draft.md", []byte(stagedBodyFile))
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	proctest.RequireStep(t, session.Report(t, instance, stagedBodyDraft(handle)), "playback")
	if staged := session.StageFile(t, handle, []byte(unseenStagedBody)); !slices.Equal(staged.Bound, []string{instance}) {
		t.Fatalf("the staging should name the capture it changed, got %v", staged.Bound)
	}

	serve = session.Answer(t, instance, "playback", "confirm", nil, "capture it")
	proctest.RequireStep(t, serve, "playback")
	if !strings.Contains(serve.Instructions, "not yet played back") || !strings.Contains(serve.Instructions, "not answered") {
		t.Fatalf("a confirm over an unseen body should serve the body instead:\n%s", serve.Instructions)
	}
	proctest.RequireStep(t, session.Answer(t, instance, "playback", "confirm", nil, "capture what I saw"), "verifySummary")
	serve = session.Answer(t, instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	entryID, _ := serve.Produced["entryId"].(string)
	if entry := proctest.LoadEntry(t, world.GraphDir, entryID); !strings.Contains(entry.Content, "not yet played back") {
		t.Errorf("the entry should carry the body played back last, got %q", entry.Content)
	}
}

func TestCapture_RefusedAnswerLeavesTheStagedBodyAlone(t *testing.T) {
	tests := []struct {
		name, chooser, choice string
	}{
		{name: "an option collecting nothing", chooser: "playback", choice: "confirm"},
		{name: "a chooser not pending", chooser: "reviseOrOverride", choice: "revise"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, session := newCaptureWorld(t, "staged-body-refused-answer")
			handle := session.Stage(t, "draft.md", []byte(stagedBodyFile))
			other := session.Stage(t, "other.md", []byte(unseenStagedBody))
			serve := session.Start(t, "capture", nil)
			instance := serve.Instance
			proctest.RequireStep(t, session.Report(t, instance, stagedBodyDraft(handle)), "playback")

			if _, err := session.AnswerErr(t, instance, tt.chooser, tt.choice, map[string]any{"bodyHandle": other}, "use the other file"); err == nil {
				t.Fatal("the answer should be refused")
			}
			resumed, err := session.WF.ServeAll(t.Context(), session.World.Identity)
			if err != nil {
				t.Fatal(err)
			}
			for _, open := range resumed.Open {
				if open.Instance == instance && strings.Contains(open.Instructions, "not yet played back") {
					t.Errorf("a refused answer should leave the body as it was:\n%s", open.Instructions)
				}
			}
		})
	}
}

func TestCapture_RestageDuringAnUnfinishedOperationIsRefused(t *testing.T) {
	_, session := newCaptureWorld(t, "staged-body-pending", proctest.WithFinalizers(&captureCompletionFailure{}))
	handle := session.Stage(t, "draft.md", []byte(stagedBodyFile))
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	proctest.RequireStep(t, session.Report(t, instance, stagedBodyDraft(handle)), "playback")
	failed, err := session.AnswerErr(t, instance, "playback", "confirm", nil, "capture it")
	if err != nil || failed.PendingOperation == nil {
		t.Fatalf("the failing finalizer should leave the write unfinished, got %v", err)
	}

	if _, err := session.StageErr(t, handle, []byte(unseenStagedBody)); err == nil || !strings.Contains(err.Error(), "unfinished") {
		t.Fatalf("a re-stage while the write is unfinished should be refused, got %v", err)
	}
	page, _, err := session.WF.ReadStagedAttachment(t.Context(), session.World.Identity, handle, 0, 0)
	if err != nil || string(page.Content) != stagedBodyFile {
		t.Errorf("a refused re-stage should leave the staged file as it was, got %q (%v)", page.Content, err)
	}
}

func TestCapture_RestageAfterPublicationBindsNothing(t *testing.T) {
	_, session := newCaptureWorld(t, "staged-body-published")
	handle := session.Stage(t, "draft.md", []byte(stagedBodyFile))
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	proctest.RequireStep(t, session.Report(t, instance, stagedBodyDraft(handle)), "playback")
	proctest.RequireStep(t, session.Answer(t, instance, "playback", "confirm", nil, "capture it"), "verifySummary")

	if staged := session.StageFile(t, handle, []byte(unseenStagedBody)); len(staged.Bound) > 0 {
		t.Errorf("a capture that wrote its entry should not take a later staging, got %v", staged.Bound)
	}
}

func TestCapture_RestagedBodySurvivesAResume(t *testing.T) {
	world, session := newCaptureWorld(t, "staged-body-replay")
	world.LLM.PreflightFindings = []proctest.PreflightFinding{{Severity: "high", Category: "test-block", Observation: "Review this finding."}}
	handle := session.Stage(t, "draft.md", []byte(stagedBodyFile))
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	proctest.RequireStep(t, session.Report(t, instance, stagedBodyDraft(handle)), "playback")
	proctest.RequireStep(t, session.Answer(t, instance, "playback", "confirm", nil, "capture it"), "reviseOrOverride")
	session.Stage(t, handle, []byte(unseenStagedBody))
	world.LLM.PreflightFindings = nil

	resumed, _ := world.Resume(t, session.ID, "staged-body-replay-resumed")
	serve = resumed.Answer(t, instance, "reviseOrOverride", "override", nil, "the finding does not apply")
	proctest.RequireStep(t, serve, "playback")
	proctest.RequireStep(t, resumed.Answer(t, instance, "playback", "confirm", nil, "capture the revision"), "verifySummary")
	serve = resumed.Answer(t, instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	entryID, _ := serve.Produced["entryId"].(string)
	if entry := proctest.LoadEntry(t, world.GraphDir, entryID); !strings.Contains(entry.Content, "not yet played back") {
		t.Errorf("the re-staged body should survive a resume, got %q", entry.Content)
	}
}
