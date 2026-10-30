package proctest_test

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
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
