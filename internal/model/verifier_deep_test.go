package model_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strconv"
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

func TestBuildDeepProposal_HunkCarriesTheChangedSpanWithAFewWordsAround(t *testing.T) {
	base := "The capture writer, which every import runs through, drops the second attachment when two attachments share a name in one staging folder.\n"
	_, hunks, err := model.BuildDeepProposal(base, []model.DeepChange{{Finding: "f1", Outcome: model.DeepApplied, Edits: []model.DeepEdit{{Old: "two attachments share", New: "two staged files share"}}}})
	if err != nil {
		t.Fatal(err)
	}
	got := model.RenderDeepHunks(hunks)
	want := "@@ h1 [f1 applied] line 1\n-…second attachment when two attachments share a name in…\n+…second attachment when two staged files share a name in…\n"
	if got != want {
		t.Fatalf("a hunk should carry the changed span and three words each side, not the paragraph:\n%s", got)
	}
}

func TestDeepSettledLine(t *testing.T) {
	finding := model.DeepFinding{Verdict: "the layer is too low", Axis: "ref-fit", Severity: "low"}
	tests := []struct {
		name, settled, note, want string
	}{
		{name: "with a note", settled: "withdrawn", note: "the dialogue\n  names the layer", want: "- f2 [low, ref-fit] withdrawn: the dialogue names the layer\n"},
		{name: "without a note", settled: "accepted without an edit", want: "- f2 [low, ref-fit] accepted without an edit\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := model.DeepSettledLine("f2", finding, tt.settled, tt.note); got != tt.want {
				t.Fatalf("a settled finding should render as one line with its label, outcome and note, got %q", got)
			}
		})
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

func TestMergeDeepBody(t *testing.T) {
	changes := []model.DeepChange{
		{Finding: "f1", Edits: []model.DeepEdit{{Old: "two share a name", New: "two share a file name"}}},
		{Finding: "f2", Edits: []model.DeepEdit{{Old: "\"tactical\"", New: "\"operational\""}}},
	}
	tests := []struct {
		name    string
		edits   []model.DeepEdit
		want    string
		refusal string
	}{
		{name: "no edit", want: "The capture writer drops the second attachment.\nIt happens when two share a file name.\n"},
		{name: "edits beside the hunks", edits: []model.DeepEdit{{Old: "The capture writer", New: "The attachment writer"}, {Old: "It happens", New: "It happens only"}},
			want: "The attachment writer drops the second attachment.\nIt happens only when two share a file name.\n"},
		{name: "an edit against the proposal's text", edits: []model.DeepEdit{{Old: "a file name.", New: "a file name, case folded."}}, refusal: "pair 1 edits text f1's edit changes"},
		{name: "an edit inside a hunk after an earlier edit moved it", edits: []model.DeepEdit{{Old: "The capture writer", New: "The capture writer, through which every import of an entry runs,"}, {Old: "share a file", New: "hold a file"}}, refusal: "pair 2 edits text f1's edit changes"},
		{name: "an edit of text a hunk rewrote", edits: []model.DeepEdit{{Old: "share a name", New: "collide"}}, refusal: "pair 1: old text is not in the proposal's body"},
		{name: "an edit matching twice", edits: []model.DeepEdit{{Old: "ha", New: "a"}}, refusal: "pair 1: old text matches the proposal's body 2 times"},
		{name: "an empty old text", edits: []model.DeepEdit{{Old: "", New: "x"}}, refusal: "pair 1: old text is empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := model.MergeDeepBody(deepBase, changes, tt.edits)
			if tt.refusal != "" {
				if err == nil || !strings.Contains(err.Error(), tt.refusal) {
					t.Fatalf("the edits should be refused by name, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if body != tt.want {
				t.Fatalf("the body should carry the proposal with the edits beside it, got %q", body)
			}
		})
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

func TestDeepEditSettled_OnlyARevertOrRestoreWhereTheEditApplied(t *testing.T) {
	base := "The parser reads one file.\nIt skips the team notes.\n"
	changes := []model.DeepChange{
		{Finding: "f1", Outcome: model.DeepApplied, Edits: []model.DeepEdit{{Old: "one", New: "the"}}},
		{Finding: "f2", Outcome: model.DeepApplied, Edits: []model.DeepEdit{{Old: "reads", New: "reads and checks"}}},
	}
	proposal := "The parser reads and checks the file.\nIt skips the team notes.\n"
	firstReverted := "The parser reads and checks one file.\nIt skips the team notes.\n"
	cases := []struct {
		name, document, draft string
		changes               []model.DeepChange
		settled               bool
	}{
		{name: "one edit reverted", document: proposal, draft: firstReverted, settled: true},
		{name: "every edit reverted", document: proposal, draft: base, settled: true},
		{name: "a reverted edit restored", document: firstReverted, draft: proposal, settled: true},
		{name: "an insertion inside an edit's text", document: proposal, draft: "The parser reads and now deletes every table and checks the file.\nIt skips the team notes.\n"},
		{name: "an edit's interior replaced", document: proposal, draft: "The parser reads and ignores the file.\nIt skips the team notes.\n"},
		{name: "an edit partly reverted", document: proposal, draft: "The parser reads and the file.\nIt skips the team notes.\n"},
		{name: "another occurrence of an edit's text", document: proposal, draft: "The parser reads and checks the file.\nIt skips thy team notes.\n"},
		{name: "a document no proposal variant", document: "The parser reads and checks the file.\nIt skips notes.\n", draft: proposal},
		{name: "edits that no longer apply", document: proposal, draft: firstReverted, changes: []model.DeepChange{{Finding: "f1", Outcome: model.DeepApplied, Edits: []model.DeepEdit{{Old: "absent", New: "the"}}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			edits := changes
			if tc.changes != nil {
				edits = tc.changes
			}
			if got := model.DeepEditSettled(base, edits, tc.document, tc.draft); got != tc.settled {
				t.Fatalf("settled should be %v for a draft that %s, got %v", tc.settled, tc.name, got)
			}
		})
	}
}

// deepLookup finds one entry, one file and one commit.
var deepLookup = model.DeepLookup{
	Entry: func(id string) (string, bool) {
		if id == "20261101-100000-s-tac-abc" {
			return "An entry.\n\nThe writer keeps the first attachment.", true
		}
		return "", false
	},
	File: func(path string) (string, bool, error) {
		if path == "internal/capture/writer.go" {
			return "Write keeps the first\n   attachment of a name.\n", true, nil
		}
		return "", false, nil
	},
	Commit: func(hash string) (string, bool, error) {
		if hash == "4aeb2c4e" {
			return "fix(capture): keep the first attachment of a name\n", true, nil
		}
		return "", false, nil
	},
}

func evidence(sources ...model.DeepSource) model.DeepFinding {
	finding := deepFinding("evidence")
	finding.Sources = sources
	return finding
}

func TestCheckDeepFindings(t *testing.T) {
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
		{"invented file", evidence(model.DeepSource{Kind: "file", Ref: "internal/capture/invented.go"}), "f1: source file internal/capture/invented.go is not in the checkout"},
		{"quote not in the file", evidence(model.DeepSource{Kind: "file", Ref: "internal/capture/writer.go", Quote: "keeps the last attachment"}), `f1: source file internal/capture/writer.go does not hold the quote "keeps the last attachment"`},
		{"invented commit", evidence(model.DeepSource{Kind: "commit", Ref: "deadbeef"}), "f1: source commit deadbeef is not in the repository"},
		{"invented entry", evidence(model.DeepSource{Kind: "entry", Ref: "20261101-100000-s-tac-zzz"}), "f1: source entry 20261101-100000-s-tac-zzz resolves to no entry"},
		{"quote not in the entry", evidence(model.DeepSource{Kind: "entry", Ref: "20261101-100000-s-tac-abc", Quote: "keeps the last attachment"}), `f1: source entry 20261101-100000-s-tac-abc does not hold the quote "keeps the last attachment"`},
		{"quote not in the commit", evidence(model.DeepSource{Kind: "commit", Ref: "4aeb2c4e", Quote: "drop the attachment"}), `f1: source commit 4aeb2c4e does not hold the quote "drop the attachment"`},
		{"malformed url", evidence(model.DeepSource{Kind: "url", Ref: "example.org/spec"}), `f1: source url "example.org/spec" is no http or https URL`},
		{"unknown kind", evidence(model.DeepSource{Kind: "chat", Ref: "the user said so"}), `f1: source kind "chat" is not one of`},
		{"path leaving the checkout", evidence(model.DeepSource{Kind: "file", Ref: "../secrets.txt"}), `f1: source file "../secrets.txt" is no path inside the checkout`},
		{"pointer quote not in the draft", model.DeepFinding{Verdict: "v", Axis: "inconsistent", Severity: "low", Pointers: []model.DeepPointer{{Field: "body", Quote: "drops every attachment"}}}, `f1: pointer quote "drops every attachment" is not in the draft`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problems, err := model.CheckDeepFindings(deepBase, []model.DeepFinding{tc.finding}, deepLookup)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Fatalf("want a problem naming %q, got %v", tc.want, problems)
			}
		})
	}
	if problems, err := model.CheckDeepFindings(deepBase, nil, deepLookup); err != nil || len(problems) != 0 {
		t.Fatalf("an empty set of findings stands, got %v, %v", problems, err)
	}
}

// Quotes match across differing whitespace, and a URL is checked for its
// form alone, never fetched.
func TestCheckDeepFindings_CitationsThatResolveStand(t *testing.T) {
	finding := evidence(
		model.DeepSource{Kind: "file", Ref: "internal/capture/writer.go", Quote: "keeps the first attachment"},
		model.DeepSource{Kind: "file", Ref: "internal/capture/writer.go"},
		model.DeepSource{Kind: "commit", Ref: "4aeb2c4e", Quote: "keep the first attachment"},
		model.DeepSource{Kind: "entry", Ref: "20261101-100000-s-tac-abc", Quote: "keeps the first attachment"},
		model.DeepSource{Kind: "url", Ref: "https://example.org/never-fetched"},
	)
	finding.Pointers = []model.DeepPointer{{Field: "body", Quote: "drops the second\n  attachment."}, {Field: "layer", Quote: "tactical"}}
	problems, err := model.CheckDeepFindings(deepBase, []model.DeepFinding{finding}, deepLookup)
	if err != nil || len(problems) != 0 {
		t.Fatalf("citations that resolve should stand, got %v, %v", problems, err)
	}
}

func TestCheckDeepFindings_PointerQuoteMatchesAHeaderFieldsValue(t *testing.T) {
	base := "title: \"The \\\"writer\\\" drops <files>\"\n\nBody.\n"
	finding := model.DeepFinding{Verdict: "v", Axis: "inconsistent", Severity: "low", Pointers: []model.DeepPointer{{Field: "title", Quote: `The "writer" drops <files>`}}}
	problems, err := model.CheckDeepFindings(base, []model.DeepFinding{finding}, deepLookup)
	if err != nil || len(problems) != 0 {
		t.Fatalf("a quote of a header field's value should match it, got %v, %v", problems, err)
	}
}

func TestCheckDeepFindings_ALookupFailureIsAnError(t *testing.T) {
	failing := deepLookup
	failing.Commit = func(string) (string, bool, error) { return "", false, errors.New("git is gone") }
	_, err := model.CheckDeepFindings(deepBase, []model.DeepFinding{evidence(model.DeepSource{Kind: "commit", Ref: "4aeb2c4e"})}, failing)
	if err == nil || !strings.Contains(err.Error(), "git is gone") {
		t.Fatalf("a lookup failure should surface as an error, got %v", err)
	}
}

// An earlier build stored each source as a plain string; such a finding
// decodes with the kind the string's form denotes and encodes as an object.
func TestDeepFinding_DecodesSourcesStoredAsStrings(t *testing.T) {
	for _, tc := range []struct {
		name     string
		source   string
		wantKind string
	}{
		{"file path", "internal/foo.go", model.DeepSourceFile},
		{"entry ID", "20261109-004729-d-cpt-xs5", model.DeepSourceEntry},
		{"short commit hash", "4aeb2c4e", model.DeepSourceCommit},
		{"full commit hash", "4aeb2c4e4aeb2c4e4aeb2c4e4aeb2c4e4aeb2c4e", model.DeepSourceCommit},
		{"URL", "https://example.com/spec", model.DeepSourceURL},
		{"string of no kind's form", "/abs/path/foo.go", model.DeepSourceFile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stored := `{"verdict":"v","axis":"evidence","severity":"medium","sources":[` + strconv.Quote(tc.source) + `]}`
			var finding model.DeepFinding
			err := json.Unmarshal([]byte(stored), &finding)
			if err != nil {
				t.Fatalf("a finding with a string source should decode: %v", err)
			}
			if len(finding.Sources) != 1 || finding.Sources[0].Kind != tc.wantKind || finding.Sources[0].Ref != tc.source {
				t.Fatalf("the string should become a source of the kind it denotes, keeping it as ref, got %+v", finding.Sources)
			}
			err = finding.Validate()
			if err != nil {
				t.Fatalf("a finding an earlier build accepted should validate: %v", err)
			}
			encoded, err := json.Marshal(finding)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(encoded), `"sources":[{"kind":`) {
				t.Fatalf("the source should encode as an object, got %s", encoded)
			}
		})
	}
}

// Sources stored as one string in place of the list decode as a one-item list.
func TestDeepFinding_DecodesSourcesStoredAsOneString(t *testing.T) {
	var finding model.DeepFinding
	err := json.Unmarshal([]byte(`{"verdict":"v","axis":"evidence","severity":"medium","sources":"internal/foo.go"}`), &finding)
	if err != nil {
		t.Fatalf("a finding with one string as sources should decode: %v", err)
	}
	if len(finding.Sources) != 1 || finding.Sources[0].Kind != model.DeepSourceFile || finding.Sources[0].Ref != "internal/foo.go" {
		t.Fatalf("the string should become a one-item list of the kind it denotes, got %+v", finding.Sources)
	}
}

// A finding only the user can settle, such as whether incidents happened, has
// nothing to cite: its missing evidence is what the user is asked for.
func TestOwnerEvidenceFindingNeedsNoSources(t *testing.T) {
	finding := model.DeepFinding{Verdict: "only the user knows whether the incidents happened", Axis: "evidence", Severity: "medium", Owner: true}
	if err := finding.Validate(); err != nil {
		t.Fatalf("an owner evidence finding without sources should validate: %v", err)
	}
	if problems, err := model.CheckDeepFindings(deepBase, []model.DeepFinding{finding}, deepLookup); err != nil || len(problems) != 0 {
		t.Fatalf("an owner evidence finding without sources should stand: %v, %v", problems, err)
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
	big := strings.Repeat("x", pageChars-10)
	pages := readingPages("draft", model.DeepContinued{},
		[]model.DeepReadingBlock{{ID: "t", Text: "target"}, {ID: "f", Text: "read in full"}},
		[]model.DeepReadingBlock{{ID: "b1", Text: "brief one"}, {ID: "b2", Text: big}, {ID: "b3", Text: "brief three"}})
	if len(pages) != 3 {
		t.Fatalf("want the lead page with the briefs that fit, then a page per brief that does not, got %d pages", len(pages))
	}
	if pages[0].Text != "draft\n\ntarget\n\nread in full\n\nbrief one" || pages[1].Text != big || pages[2].Text != "brief three" {
		t.Fatalf("page 1 holds the draft, the full reads and the brief that fits, got %q", []string{pages[0].Text, pages[1].Text[:9], pages[2].Text})
	}
	for _, page := range pages {
		if model.ServedChars(page.Text) > pageChars {
			t.Fatalf("a page should stay within the page bound, got %d", model.ServedChars(page.Text))
		}
	}
}

// An entry larger than a page runs on across pages of its own, each within
// the page bound, and its pages joined without their openings are the entry
// again; the last of them names it as served in full.
func TestDeepReadingPagesSplitAnOversizedEntry(t *testing.T) {
	huge := strings.Repeat(strings.Repeat("y", 99)+"\n", pageChars/50)
	brief := strings.Repeat("b", pageChars-10)
	pages := readingPages("draft", model.DeepContinued{}, []model.DeepReadingBlock{{ID: "h", Text: huge}}, []model.DeepReadingBlock{{ID: "b", Text: brief}})
	if len(pages) < 4 || pages[0].Text != "draft" || pages[len(pages)-1].Text != brief {
		t.Fatalf("the entry should run on across pages of its own between the lead and the briefs, got %d pages", len(pages))
	}
	var joined strings.Builder
	for i, page := range pages[1 : len(pages)-1] {
		if model.ServedChars(page.Text) > pageChars {
			t.Fatalf("page %d should stay within the page bound, got %d", i+2, model.ServedChars(page.Text))
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
	runes := "x" + strings.Repeat("ü", pageChars)
	for _, tc := range []struct {
		name, text, breaks string
		pages              int
	}{
		{"short", "the rules", "", 1},
		{"paragraphs", strings.Repeat(paragraph, pageChars*5/2/model.ServedChars(paragraph)), "\n\n", 3},
		{"lines", strings.Repeat(line, pageChars*5/2/model.ServedChars(line)), "\n", 3},
		{"one long line", runes, "", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pages := model.DeepContinuedPages("# Rules\n\n", "# Rules, continued\n\n", tc.text, model.DefaultResultBound)
			if len(pages) != tc.pages {
				t.Fatalf("the text should fill as many pages as it needs, got %d", len(pages))
			}
			var joined strings.Builder
			for i, page := range pages {
				if model.ServedChars(page.Text) > pageChars {
					t.Fatalf("page %d should stay within the page bound, got %d", i+1, model.ServedChars(page.Text))
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
	big := strings.Repeat("x", pageChars-5)
	pages := readingPages("draft", model.DeepContinued{},
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

// Briefs fill the room the lead page, bounded tighter, and the rules' last
// page leave; a block moves whole to the next page when it does not fit.
func TestDeepReadingPagesPackBriefsIntoTheRoomLeft(t *testing.T) {
	block := func(id string, chars int) model.DeepReadingBlock {
		header := "ID: " + id + "\n"
		return model.DeepReadingBlock{ID: id, Text: header + strings.Repeat("x", chars-len(header))}
	}
	var briefs []model.DeepReadingBlock
	for i := range 12 {
		briefs = append(briefs, block("b"+strconv.Itoa(i+1), 1464))
	}
	rules := model.DeepContinued{Text: strings.Repeat("r", 10996) + "\n\n" + strings.Repeat("r", 5000)}
	for _, tc := range []struct {
		name  string
		rules model.DeepContinued
		want  [][]string
	}{
		{
			name: "without rules",
			want: [][]string{{"t", "b1", "b2", "b3", "b4"}, {"b5", "b6", "b7", "b8", "b9", "b10", "b11", "b12"}},
		},
		{
			name:  "with rules",
			rules: rules,
			want:  [][]string{{"t", "b1", "b2", "b3", "b4"}, nil, {"b5", "b6", "b7", "b8"}, {"b9", "b10", "b11", "b12"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := block("t", 2000)
			pages := readingPages(strings.Repeat("d", 500), tc.rules, []model.DeepReadingBlock{target}, briefs)
			if len(pages) != len(tc.want) {
				t.Fatalf("briefs should fill the room left on earlier pages, got %d pages", len(pages))
			}
			for i, page := range pages {
				var ids []string
				for _, b := range append([]model.DeepReadingBlock{target}, briefs...) {
					if strings.Contains(page.Text, b.Text) {
						ids = append(ids, b.ID)
					}
				}
				if !slices.Equal(ids, tc.want[i]) {
					t.Errorf("page %d should hold these whole blocks, got %v", i+1, ids)
				}
				if model.ServedChars(page.Text) > pageChars {
					t.Errorf("page %d should stay within the page bound, got %d", i+1, model.ServedChars(page.Text))
				}
			}
			if !slices.Equal(pages[0].FullIDs, []string{"t"}) {
				t.Errorf("page 1 should name only the target as served in full, got %v", pages[0].FullIDs)
			}
		})
	}
}

// A lead larger than a page runs on across pages of its own before the rules,
// each within the page bound and each later one opening with the
// continuation marker. A page break inside a fence closes the fence on its
// page and reopens it on the next, so every page reads as balanced Markdown,
// and the pages joined without their openings and the added fences are the
// lead again.
func TestDeepReadingPagesRunAnOversizedLeadOn(t *testing.T) {
	const marker = "(continued from the page before)\n\n"
	draft := strings.Repeat(strings.Repeat("d", 99)+"\n", pageChars/60)
	lead := "## Draft\n\n```\n" + draft + "the draft's last line\n```\n\n## Already raised by pre-flight\n\n- [low] tone: terse\n"
	rules := model.DeepReadingPage{Text: "## Pre-flight's rules\n\nrule"}
	pages := readingPages(lead, model.DeepContinued{Intro: "## Pre-flight's rules\n\n", Text: "rule"}, []model.DeepReadingBlock{{ID: "t", Text: "target"}}, nil)
	for i, page := range pages {
		bound := pageChars
		if i == 0 {
			bound = firstPageChars
		}
		if model.ServedChars(page.Text) > bound {
			t.Fatalf("page %d should stay within its page bound, nothing cut at serving, got %d", i+1, model.ServedChars(page.Text))
		}
		if fenceLines(page.Text)%2 != 0 {
			t.Fatalf("page %d should close every fence it opens:\n%s", i+1, page.Text)
		}
	}
	last := slices.IndexFunc(pages, func(page model.DeepReadingPage) bool { return strings.Contains(page.Text, "- [low] tone: terse") })
	if last < 1 || len(pages) != last+2 || pages[last+1].Text != rules.Text {
		t.Fatalf("the lead should run on across pages of its own, the rules after it, got %d pages, the lead ending on page %d", len(pages), last+1)
	}
	if !slices.Equal(pages[last].FullIDs, []string{"t"}) {
		t.Fatalf("the lead's last page should take the target that fits, got %v", pages[last].FullIDs)
	}
	var joined strings.Builder
	for i, page := range pages[:last+1] {
		body := page.Text
		if i > 0 {
			var ok bool
			body, ok = strings.CutPrefix(body, marker+"```\n")
			if !ok {
				t.Fatalf("page %d should open with the continuation marker and reopen the fence:\n%.80s", i+1, page.Text)
			}
		}
		if i < last {
			var ok bool
			body, ok = strings.CutSuffix(body, "```\n")
			if !ok {
				t.Fatalf("page %d should close the fence its break splits", i+1)
			}
		} else {
			body = strings.TrimSuffix(body, "\n\ntarget")
		}
		joined.WriteString(body)
	}
	if joined.String() != lead {
		t.Fatal("the lead's pages joined should be the lead again, nothing cut")
	}
}

// A lead that fits a later page but not the first runs on to a second page.
func TestDeepReadingPagesRunOnALeadPastTheFirstPage(t *testing.T) {
	lead := strings.Repeat(strings.Repeat("d", 99)+"\n", (firstPageChars+pageChars)/200)
	pages := readingPages(lead, model.DeepContinued{}, nil, nil)
	if len(pages) != 2 || model.ServedChars(pages[0].Text) > firstPageChars {
		t.Fatalf("the lead should run on past the first page's tighter bound, got %d pages", len(pages))
	}
}

// A fence broken inside one long line still closes on its own line, and the
// added fence lines keep every page within the bound.
func TestDeepContinuedPagesCloseAFenceBrokenMidLine(t *testing.T) {
	pages := model.DeepContinuedPages("# Draft\n\n", "# Draft, continued\n\n", "```\n"+strings.Repeat("x", pageChars*2)+"\n```\n", model.DefaultResultBound)
	if len(pages) < 3 || !strings.HasSuffix(pages[1].Text, "x\n```\n") {
		t.Fatalf("the long line should break mid-line and close its fence on a line of its own, got %d pages", len(pages))
	}
	for i, page := range pages {
		if model.ServedChars(page.Text) > pageChars {
			t.Fatalf("page %d should stay within the page bound with its added fences, got %d", i+1, model.ServedChars(page.Text))
		}
		if i > 0 && strings.Split(page.Text, "\n")[2] != "```" {
			t.Fatalf("page %d should reopen the fence after its opening:\n%.40s", i+1, page.Text)
		}
		if fenceLines(page.Text)%2 != 0 {
			t.Fatalf("page %d should close every fence it opens", i+1)
		}
	}
}

// A reading paged for Claude Code's larger bound takes fewer pages than for
// any other host, each within that bound's page sizes, and both pagings serve
// the same entries in full.
func TestDeepReadingPagesFollowTheResultBound(t *testing.T) {
	var first, rest []model.DeepReadingBlock
	for i := range 40 {
		first = append(first, model.DeepReadingBlock{ID: "f" + strconv.Itoa(i), Text: strings.Repeat("f", 2000)})
		rest = append(rest, model.DeepReadingBlock{ID: "b" + strconv.Itoa(i), Text: strings.Repeat("b", 900)})
	}
	reading := model.DeepReading{Lead: strings.Repeat("d", 3000), Rules: model.DeepContinued{Intro: "# Rules\n\n", Continued: "# Rules, continued\n\n", Text: strings.Repeat("r\n", 15000)}, First: first, Rest: rest}
	full := func(pages []model.DeepReadingPage) []string {
		var ids []string
		for _, page := range pages {
			ids = append(ids, page.FullIDs...)
		}
		return ids
	}
	small, large := reading.Pages(model.DefaultResultBound), reading.Pages(model.ClaudeCodeResultBound)
	if len(large) >= len(small) {
		t.Fatalf("the larger bound should take fewer pages, got %d against %d", len(large), len(small))
	}
	for i, page := range large {
		room := model.ClaudeCodeResultBound.DeepPage()
		if i == 0 {
			room = model.ClaudeCodeResultBound.DeepFirstPage()
		}
		if model.ServedChars(page.Text) > room {
			t.Fatalf("page %d should stay within the larger bound's page size, got %d", i+1, model.ServedChars(page.Text))
		}
	}
	if !slices.Equal(full(small), full(large)) {
		t.Fatal("both pagings should serve the same entries in full")
	}
}

func readingPages(lead string, rules model.DeepContinued, first, rest []model.DeepReadingBlock) []model.DeepReadingPage {
	return model.DeepReading{Lead: lead, Rules: rules, First: first, Rest: rest}.Pages(model.DefaultResultBound)
}

var (
	pageChars      = model.DefaultResultBound.DeepPage()
	firstPageChars = model.DefaultResultBound.DeepFirstPage()
)

func fenceLines(text string) int {
	n := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "```") {
			n++
		}
	}
	return n
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
