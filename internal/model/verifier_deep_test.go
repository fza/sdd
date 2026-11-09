package model_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

const deepBase = "entryKind: \"gap\"\nlayer: \"tactical\"\n\nThe capture writer drops the second attachment.\nIt happens when two share a name.\n"

func deepFinding(axis string, edits ...model.DeepEdit) model.DeepFinding {
	return model.DeepFinding{Verdict: "the draft misstates it", Axis: axis, Severity: "medium", Edits: edits}
}

func TestBuildDeepProposal_LabelsEachHunkByItsFinding(t *testing.T) {
	findings := []model.DeepFinding{
		deepFinding("contradicts", model.DeepEdit{Old: "two share a name", New: "two share a file name"}),
		deepFinding("ref-fit", model.DeepEdit{Old: "\"tactical\"", New: "\"operational\""}),
	}
	proposal, hunks, err := model.BuildDeepProposal(deepBase, model.DeepChanges(findings, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(proposal, "layer: \"operational\"") || !strings.Contains(proposal, "two share a file name") {
		t.Fatalf("the proposal should carry both edits:\n%s", proposal)
	}
	if len(hunks) != 2 || hunks[0].Finding != "f2" || hunks[0].ID != "h1" || hunks[1].Finding != "f1" || hunks[1].Line != 5 {
		t.Fatalf("hunks should come in document order, each naming its finding and line, got %+v", hunks)
	}
	if rendered := model.RenderDeepHunks(hunks); !strings.Contains(rendered, "@@ h2 [f1 applied] line 5\n-It happens when two share a name.") {
		t.Fatalf("the rendered diff should label each hunk, got:\n%s", rendered)
	}
}

func TestBuildDeepProposal_RefusesNonMatchingAndOverlappingEdits(t *testing.T) {
	_, _, err := model.BuildDeepProposal(deepBase, []model.DeepChange{{Finding: "f1", Edits: []model.DeepEdit{{Old: "absent text", New: "x"}}}})
	if err == nil || !strings.Contains(err.Error(), "f1 edit 1: old text matches the draft 0 times") {
		t.Fatalf("an edit matching nothing should be refused, got %v", err)
	}
	_, _, err = model.BuildDeepProposal(deepBase, []model.DeepChange{{Finding: "f1", Edits: []model.DeepEdit{{Old: "\"", New: "'"}}}})
	if err == nil || !strings.Contains(err.Error(), "f1 edit 1: old text matches the draft 4 times") {
		t.Fatalf("an edit matching more than once should be refused, got %v", err)
	}
	_, _, err = model.BuildDeepProposal(deepBase, []model.DeepChange{
		{Finding: "f1", Edits: []model.DeepEdit{{Old: "second attachment", New: "x"}}},
		{Finding: "f2", Edits: []model.DeepEdit{{Old: "the second", New: "y"}}},
	})
	if err == nil || !strings.Contains(err.Error(), "f2 and f1 edit overlapping text") {
		t.Fatalf("overlapping edits should be refused naming both findings, got %v", err)
	}
}

func TestDeepChanges_FollowTheLatestResponse(t *testing.T) {
	findings := []model.DeepFinding{
		deepFinding("contradicts", model.DeepEdit{Old: "drops", New: "loses"}),
		deepFinding("ref-fit", model.DeepEdit{Old: "second", New: "later"}),
		{Verdict: "only the owner knows the scope", Axis: "aspiration", Severity: "low", Owner: true},
	}
	responses := []model.DeepResponse{
		{Target: "f1", Outcome: model.DeepHeld, Note: "the source says so"},
		{Target: "f2", Outcome: model.DeepRevised, Edits: []model.DeepEdit{{Old: "second", New: "next"}}},
		{Target: "f1", Outcome: model.DeepDropped, Note: "the drafter's reason holds"},
	}
	changes := model.DeepChanges(findings, responses)
	if len(changes) != 1 || changes[0].Finding != "f2" || changes[0].Outcome != model.DeepRevised || changes[0].Edits[0].New != "next" {
		t.Fatalf("the latest response per finding should decide its edits and an owner finding none, got %+v", changes)
	}
}

func TestCheckDeepFindings(t *testing.T) {
	resolves := func(id string) bool { return id == "20261101-100000-s-tac-abc" }
	for _, tc := range []struct {
		name    string
		finding model.DeepFinding
		want    string
	}{
		{"unknown axis", deepFinding("style"), `f1: axis "style" is not one of`},
		{"owner with edits", model.DeepFinding{Verdict: "v", Axis: "aspiration", Severity: "low", Owner: true, Edits: []model.DeepEdit{{Old: "drops", New: "x"}}}, "f1: an owner finding carries no edits"},
		{"evidence without sources", deepFinding("evidence"), "f1: an evidence finding names its sources"},
		{"unresolved basis", model.DeepFinding{Verdict: "v", Axis: "duplicates", Severity: "high", Basis: []string{"20261101-100000-s-tac-zzz"}}, "f1: basis 20261101-100000-s-tac-zzz resolves to no entry"},
		{"edit not matching", deepFinding("contradicts", model.DeepEdit{Old: "nowhere", New: "x"}), "f1 edit 1: old text matches the draft 0 times"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problems := model.CheckDeepFindings(deepBase, []model.DeepFinding{tc.finding}, resolves)
			if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Fatalf("want a problem naming %q, got %v", tc.want, problems)
			}
		})
	}
	if problems := model.CheckDeepFindings(deepBase, nil, resolves); len(problems) != 0 {
		t.Fatalf("an empty set of findings stands, got %v", problems)
	}
}

func TestDeepAnswersAndResponsesValidate(t *testing.T) {
	if err := (model.DeepAnswer{Target: "f1", Verdict: "contest"}).Validate(); err == nil {
		t.Fatal("a contest without a reason should be refused")
	}
	if err := (model.DeepAnswer{Target: "h1", Verdict: "accept"}).Validate(); err == nil {
		t.Fatal("an answer names a finding, not a hunk")
	}
	if err := (model.DeepResponse{Target: "f1", Outcome: model.DeepHeld}).Validate(); err == nil {
		t.Fatal("a held finding explains itself")
	}
	if err := (model.DeepResponse{Target: "f1", Outcome: model.DeepDropped, Note: "n", Edits: []model.DeepEdit{{Old: "a", New: "b"}}}).Validate(); err == nil {
		t.Fatal("a dropped finding carries no edits")
	}
	if err := (model.DeepResponse{Target: "f2", Outcome: model.DeepRevised, Edits: []model.DeepEdit{{Old: "a", New: "b"}}}).Validate(); err != nil {
		t.Fatalf("a revision needs no note: %v", err)
	}
}

func TestWeighDeep(t *testing.T) {
	found := func(path string) bool { return path == "internal/engine/spec.go" }
	for _, tc := range []struct {
		name  string
		draft model.DeepDraft
		run   bool
		want  string
	}{
		{"path in the checkout", model.DeepDraft{Text: "The gate in `internal/engine/spec.go` refuses it.", Kind: "gap", Layer: model.LayerOperational}, true, "internal/engine/spec.go"},
		{"path not in the checkout", model.DeepDraft{Text: "The gate in `internal/none.go` refuses it.", Kind: "gap", Layer: model.LayerOperational}, false, "no claim a tool can check"},
		{"url", model.DeepDraft{Text: "See https://example.org/spec for it.", Kind: "gap", Layer: model.LayerOperational}, true, "https://example.org/spec"},
		{"commit", model.DeepDraft{Text: "Commit 4aeb2c4e broke it.", Kind: "gap", Layer: model.LayerOperational}, true, "commit 4aeb2c4e"},
		{"conceptual directive", model.DeepDraft{Text: "Do it.", Kind: model.KindDirective, Layer: model.LayerConceptual}, true, "directive on the conceptual layer"},
		{"operational plan", model.DeepDraft{Text: "Do it.", Kind: model.KindPlan, Layer: model.LayerOperational}, false, "no claim"},
		{"closes", model.DeepDraft{Text: "Done.", Kind: "done", Layer: model.LayerOperational, Closes: []string{"20261101-100000-s-tac-abc"}}, true, "supersedes or closes"},
		{"medium findings", model.DeepDraft{Text: "Seen.", Kind: "gap", Layer: model.LayerOperational, MediumFindings: 2}, true, "2 medium finding(s)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			weighing := model.WeighDeep(tc.draft, found)
			if weighing.Run != tc.run || !strings.Contains(strings.Join(weighing.Reasons, "; "), tc.want) {
				t.Fatalf("want run %v naming %q, got %+v", tc.run, tc.want, weighing)
			}
		})
	}
}

func TestDeepReadingPages(t *testing.T) {
	big := strings.Repeat("x", model.DeepPageChars-20)
	pages := model.DeepReadingPages("draft", []string{"target", "read in full"}, []string{"brief one", big, "brief three"})
	if len(pages) != 3 {
		t.Fatalf("want the lead page, then the briefs paged, got %d pages", len(pages))
	}
	if pages[0] != "draft\n\ntarget\n\nread in full" || !strings.HasPrefix(pages[1], "brief one") || pages[2] != "brief three" {
		t.Fatalf("page 1 holds the draft and full reads, the briefs follow on pages of their own, got %q", []string{pages[0], pages[1][:9], pages[2]})
	}
	for _, page := range pages {
		if len(page) > model.DeepPageChars {
			t.Fatalf("a page should stay within %d characters, got %d", model.DeepPageChars, len(page))
		}
	}
	huge := strings.Repeat("y", model.DeepPageChars+5)
	pages = model.DeepReadingPages("draft", nil, []string{huge})
	if len(pages) != 2 || pages[1] != huge {
		t.Fatal("a block larger than a page should have a page of its own, never cut")
	}
}

func TestMarkVerificationSkipped(t *testing.T) {
	for _, tc := range []struct {
		cause, confidence, want string
	}{
		{model.DeepSkipFailure, "high", "low"},
		{model.DeepSkipWeighed, "high", "medium"},
		{model.DeepSkipWeighed, "low", "low"},
	} {
		entry := &model.Entry{Confidence: tc.confidence}
		if err := model.MarkVerificationSkipped(entry, tc.cause); err != nil {
			t.Fatal(err)
		}
		if entry.Confidence != tc.want || entry.Verification != model.VerificationSkipped {
			t.Errorf("a %s skip of a %s entry should cap its confidence and mark it skipped, got %s and %q", tc.cause, tc.confidence, entry.Confidence, entry.Verification)
		}
	}
	if err := model.MarkVerificationSkipped(&model.Entry{}, "tired"); err == nil {
		t.Fatal("an unknown cause should be refused")
	}
}

func TestDeepChanges_LeaveOwnerFindingsToTheUser(t *testing.T) {
	findings := []model.DeepFinding{{Verdict: "only the owner knows the scope", Axis: "aspiration", Severity: "low", Owner: true, Edits: []model.DeepEdit{{Old: "drops", New: "loses"}}}}
	if changes := model.DeepChanges(findings, nil); len(changes) != 0 {
		t.Fatalf("an owner finding should carry nothing into the proposal, got %+v", changes)
	}
}
