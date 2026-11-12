package model_test

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

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

func TestDeepOpenFindings_FollowTheLatestResponse(t *testing.T) {
	findings := []model.DeepFinding{deepFinding("contradicts"), deepFinding("ref-fit"), deepFinding("evidence"), deepFinding("strain")}
	responses := []model.DeepResponse{
		{Target: "f1", Outcome: model.DeepDropped},
		{Target: "f2", Outcome: model.DeepConceded},
		{Target: "f3", Outcome: model.DeepDropped},
		{Target: "f3", Outcome: model.DeepHeld},
	}
	if open := model.DeepOpenFindings(findings, responses); open != 2 {
		t.Fatalf("a finding stays open unless its latest response concedes or drops it, counted %d", open)
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
	pages := model.DeepReadingPages("draft",
		[]model.DeepReadingBlock{{ID: "t", Text: "target"}, {ID: "f", Text: "read in full"}},
		[]model.DeepReadingBlock{{ID: "b1", Text: "brief one"}, {ID: "b2", Text: big}, {ID: "b3", Text: "brief three"}})
	if len(pages) != 3 {
		t.Fatalf("want the lead page, then the briefs paged, got %d pages", len(pages))
	}
	if pages[0].Text != "draft\n\ntarget\n\nread in full" || !strings.HasPrefix(pages[1].Text, "brief one") || pages[2].Text != "brief three" {
		t.Fatalf("page 1 holds the draft and full reads, the briefs follow on pages of their own, got %q", []string{pages[0].Text, pages[1].Text[:9], pages[2].Text})
	}
	for _, page := range pages {
		if len(page.Text) > model.DeepPageChars {
			t.Fatalf("a page should stay within %d characters, got %d", model.DeepPageChars, len(page.Text))
		}
	}
}

// An entry larger than a page runs on across pages of its own, each within
// the page bound, and its pages joined without their openings are the entry
// again; the last of them names it as served in full.
func TestDeepReadingPagesSplitAnOversizedEntry(t *testing.T) {
	huge := strings.Repeat(strings.Repeat("y", 99)+"\n", model.DeepPageChars/50)
	pages := model.DeepReadingPages("draft", []model.DeepReadingBlock{{ID: "h", Text: huge}}, []model.DeepReadingBlock{{ID: "b", Text: "brief"}})
	if len(pages) < 4 || pages[0].Text != "draft" || pages[len(pages)-1].Text != "brief" {
		t.Fatalf("the entry should run on across pages of its own between the lead and the briefs, got %d pages", len(pages))
	}
	var joined strings.Builder
	for i, page := range pages[1 : len(pages)-1] {
		if len(page.Text) > model.DeepPageChars {
			t.Fatalf("page %d should stay within the page bound, got %d characters", i+2, len(page.Text))
		}
		body := page.Text
		if i > 0 {
			var ok bool
			body, ok = strings.CutPrefix(page.Text, "(h, continued)\n\n")
			if !ok {
				t.Fatalf("page %d should open naming the entry it continues:\n%.40s", i+2, page.Text)
			}
		}
		joined.WriteString(body)
		last := i == len(pages)-3
		if slices.Contains(page.FullIDs, "h") != last {
			t.Fatalf("only the entry's last page should name it as served in full, page %d names %v", i+2, page.FullIDs)
		}
	}
	if joined.String() != huge {
		t.Fatal("the pages joined should be the entry again, nothing cut")
	}
}

func TestDeepContinuedPages(t *testing.T) {
	line := strings.Repeat("l", 99) + "\n"
	paragraph := strings.Repeat(line, 10) + "\n"
	runes := "x" + strings.Repeat("ü", model.DeepPageChars)
	for _, tc := range []struct {
		name, text, breaks string
		pages              int
	}{
		{"short", "the rules", "", 1},
		{"paragraphs", strings.Repeat(paragraph, 120), "\n\n", 3},
		{"lines", strings.Repeat(line, 1200), "\n", 3},
		{"one long line", runes, "", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pages := model.DeepContinuedPages("# Rules\n\n", "# Rules, continued\n\n", tc.text)
			if len(pages) != tc.pages {
				t.Fatalf("the text should fill as many pages as it needs, got %d", len(pages))
			}
			var joined strings.Builder
			for i, page := range pages {
				if len(page.Text) > model.DeepPageChars {
					t.Fatalf("page %d should stay within the page bound, got %d characters", i+1, len(page.Text))
				}
				opening := "# Rules, continued\n\n"
				if i == 0 {
					opening = "# Rules\n\n"
				}
				body, ok := strings.CutPrefix(page.Text, opening)
				if !ok {
					t.Fatalf("page %d should open with its heading:\n%.40s", i+1, page.Text)
				}
				if i < len(pages)-1 && !strings.HasSuffix(body, tc.breaks) {
					t.Fatalf("page %d should break at the widest break the text offers", i+1)
				}
				if !utf8.ValidString(body) {
					t.Fatalf("page %d should break between runes", i+1)
				}
				joined.WriteString(body)
			}
			if joined.String() != tc.text {
				t.Fatal("the pages joined should be the text again, nothing cut")
			}
		})
	}
}

// Each page names the entries it serves in full: the blocks of first, on the
// page they land on; briefs name none.
func TestDeepReadingPagesNameTheirFullEntries(t *testing.T) {
	big := strings.Repeat("x", model.DeepPageChars-5)
	pages := model.DeepReadingPages("draft",
		[]model.DeepReadingBlock{{ID: "t", Text: "target"}, {ID: "f", Text: big}},
		[]model.DeepReadingBlock{{ID: "b1", Text: "brief one"}})
	want := [][]string{{"t"}, {"f"}, nil}
	if len(pages) != len(want) {
		t.Fatalf("want a full read too large for page 1 on a page of its own, then the briefs, got %d pages", len(pages))
	}
	for i, page := range pages {
		if !slices.Equal(page.FullIDs, want[i]) {
			t.Fatalf("page %d should name the entries it serves in full, got %v", i+1, page.FullIDs)
		}
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

func TestParseDeepDraftDocument(t *testing.T) {
	parsed, err := model.ParseDeepDraftDocument(deepBase)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Fields["entryKind"] != "gap" || parsed.Fields["layer"] != "tactical" || len(parsed.Fields) != 2 {
		t.Fatalf("each header line should read back as its field, got %v", parsed.Fields)
	}
	if !parsed.HasBody || parsed.Body != "The capture writer drops the second attachment.\nIt happens when two share a name.\n" {
		t.Fatalf("the text after the empty line should read back as the body, got %q", parsed.Body)
	}
	refs, err := model.ParseDeepDraftDocument("refs: [{\"desc\":\"while <limit> & more\",\"id\":\"x\"}]\n\nbody")
	if err != nil {
		t.Fatal(err)
	}
	if list, _ := refs.Fields["refs"].([]any); len(list) != 1 {
		t.Fatalf("a JSON list should read back as a list, got %v", refs.Fields["refs"])
	}
	for _, tc := range []struct{ doc, want string }{
		{"layer \"tactical\"\n\nbody", "is not `field: value`"},
		{"layer: \"tactical\"\nlayer: \"process\"\n\nbody", "appears twice"},
		{"layer: tactical\n\nbody", "header field layer"},
	} {
		_, err := model.ParseDeepDraftDocument(tc.doc)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ParseDeepDraftDocument(%q) = %v, want a refusal naming %q", tc.doc, err, tc.want)
		}
	}
}
