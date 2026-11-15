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
		{
			name:   "a header stating a reported field otherwise",
			file:   "---\nconfidence: high\n---\nBody.\n",
			report: stagedBodyDraft,
			want:   `"confidence"`,
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

func TestCapture_RestageContradictingABoundCaptureIsRefused(t *testing.T) {
	_, session := newCaptureWorld(t, "staged-body-contradiction")
	staged := session.StageFile(t, "draft.md", []byte(stagedBodyFile))
	serve := session.Start(t, "capture", nil)
	session.Report(t, serve.Instance, stagedBodyDraft(staged.Handle))

	_, err := session.StageErr(t, staged.Handle, []byte("---\nlayer: strategic\n---\nOther body.\n"))
	if err == nil || !strings.Contains(err.Error(), `"layer"`) {
		t.Fatalf("a re-stage contradicting the bound capture should be refused naming the key, got %v", err)
	}
	page, _, err := session.WF.ReadStagedAttachment(t.Context(), session.World.Identity, staged.Handle, 0, 0)
	if err != nil || string(page.Content) != stagedBodyFile {
		t.Errorf("a refused re-stage should leave the staged file as it was, got %q (%v)", page.Content, err)
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

// The MCP server replays the session before every call, so the unserved
// change must survive the replay.
func TestCapture_RestageIsPlayedBackAcrossRefreshes(t *testing.T) {
	_, session := newCaptureWorld(t, "staged-body-refresh-restage")
	handle := session.Stage(t, "draft.md", []byte(stagedBodyFile))
	instance := session.Start(t, "capture", nil).Instance
	session.Refresh(t)
	proctest.RequireStep(t, session.Report(t, instance, stagedBodyDraft(handle)), "playback")
	session.Refresh(t)
	session.Stage(t, handle, []byte(unseenStagedBody))
	session.Refresh(t)

	serve := session.Answer(t, instance, "playback", "confirm", nil, "capture it")
	proctest.RequireStep(t, serve, "playback")
	if !strings.Contains(serve.Instructions, "not answered") || !strings.Contains(serve.Instructions, "not yet played back") {
		t.Fatalf("a confirm after a re-stage should play the change back instead:\n%s", serve.Instructions)
	}
	session.Refresh(t)
	proctest.RequireStep(t, session.Answer(t, instance, "playback", "confirm", nil, "yes, that one"), "verifySummary")
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

// A playback whose mark another writer's append displaced fails loud, so the
// client never takes a playback the log does not hold.
func TestCapture_PlaybackOnAStaleSessionFails(t *testing.T) {
	_, session := newCaptureWorld(t, "staged-body-stale-playback")
	handle := session.Stage(t, "draft.md", []byte(stagedBodyFile))
	instance := session.Start(t, "capture", nil).Instance
	proctest.RequireStep(t, session.Report(t, instance, stagedBodyDraft(handle)), "playback")
	session.Stage(t, handle, []byte(unseenStagedBody))
	other := &proctest.Session{World: session.World, WF: session.WF, ID: session.ID}
	other.Refresh(t)
	other.LogRead(t, "show", []string{captureRefID}, nil)

	position, err := session.WF.ServeAll(t.Context(), session.World.Identity)
	if err != nil {
		t.Fatal(err)
	}
	var playbacks []*sdd.WorkflowServe
	for i := range position.Open {
		playbacks = append(playbacks, &position.Open[i])
	}
	if err := session.WF.RecordPlayback(t.Context(), session.World.Identity, playbacks...); err == nil {
		t.Fatal("a playback whose mark cannot be appended should fail")
	}
	if _, err := session.WF.ServeAll(t.Context(), session.World.Identity); err == nil {
		t.Error("a serve after the failed mark should fail too")
	}
}
