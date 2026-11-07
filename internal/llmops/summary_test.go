package llmops_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

func titledFixture() (*model.Entry, *model.Graph) {
	related := &model.Entry{
		ID: "20260101-100000-s-tac-rel", Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical,
		Title: "daemon project removal", Summary: "The daemon project is slated for removal.", Content: "Remove it.",
	}
	entry := &model.Entry{
		ID: "20260102-100000-d-tac-ent", Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerTactical,
		Refs: []model.Ref{{ID: related.ID, Kind: model.RefKindAddresses, Desc: "the ref description"}}, Summary: "A stored summary that may have been corrected.",
		Content: "The daemon project goes.",
	}
	return entry, model.NewGraph([]*model.Entry{related, entry})
}

func untitledCopy(entry *model.Entry) *model.Entry {
	untitled := *entry
	untitled.Title = ""
	return &untitled
}

func unsummarizedCopy(entry *model.Entry) *model.Entry {
	unsummarized := *entry
	unsummarized.Summary = ""
	unsummarized.Title = ""
	return &unsummarized
}

func TestNewDescriptionChoosesTheCall(t *testing.T) {
	entry, graph := titledFixture()
	cases := []struct {
		name          string
		entry         *model.Entry
		regenerate    bool
		wantPurpose   llm.Purpose
		wantTitleOnly bool
	}{
		{name: "no summary", entry: unsummarizedCopy(entry), wantPurpose: llm.PurposeSummarize},
		{name: "summary without title", entry: untitledCopy(entry), wantPurpose: llm.PurposeTitle, wantTitleOnly: true},
		{name: "summary without title, regenerated", entry: untitledCopy(entry), regenerate: true, wantPurpose: llm.PurposeSummarize},
		{name: "titled and summarized, regenerated", entry: entry, regenerate: true, wantPurpose: llm.PurposeSummarize},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			description, err := llmops.NewDescription(tc.entry, graph, "", tc.regenerate)
			if err != nil {
				t.Fatalf("NewDescription: %v", err)
			}
			if description.Request.Purpose != tc.wantPurpose || description.TitleOnly() != tc.wantTitleOnly {
				t.Errorf("the entry should get the call its state needs, got purpose %q, title only %v", description.Request.Purpose, description.TitleOnly())
			}
		})
	}
}

func TestNewDescriptionRefusesADescribedEntry(t *testing.T) {
	entry, graph := titledFixture()
	entry.Title = "Daemon removal"
	_, err := llmops.NewDescription(entry, graph, "", false)
	if !errors.Is(err, llmops.ErrDescribed) {
		t.Fatalf("an entry holding title and summary should be refused unless regenerated, got %v", err)
	}
}

func TestPairedDescriptionShowsRelatedTitles(t *testing.T) {
	entry, graph := titledFixture()
	description, err := llmops.NewDescription(unsummarizedCopy(entry), graph, "", false)
	if err != nil {
		t.Fatalf("NewDescription: %v", err)
	}
	req := description.Request
	if !strings.Contains(req.UserPrompt, "Title: daemon project removal") {
		t.Errorf("related entries should carry their titles, so a differing subject can be named apart:\n%s", req.UserPrompt)
	}
	if !strings.Contains(req.UserPrompt, entry.Content) {
		t.Errorf("the paired call should read the full entry:\n%s", req.UserPrompt)
	}
	if !strings.Contains(req.SystemPrompt, `{"title": "...", "summary": "..."}`) {
		t.Errorf("the prompt should ask for the paired answer:\n%s", req.SystemPrompt)
	}
}

func TestTitleDescriptionSendsOnlyWhatATitleNames(t *testing.T) {
	entry, graph := titledFixture()
	related := graph.ByID["20260101-100000-s-tac-rel"]
	description, err := llmops.NewDescription(untitledCopy(entry), graph, "de", false)
	if err != nil {
		t.Fatalf("NewDescription: %v", err)
	}
	req := description.Request
	combined := req.Combined()
	for _, want := range []string{entry.Summary, "Type: decision", "Layer: tactical", "Kind: directive", related.Title, "`de`"} {
		if !strings.Contains(combined, want) {
			t.Errorf("the title call should carry %q:\n%s", want, combined)
		}
	}
	for _, unwanted := range []string{entry.Content, related.Summary, related.Content, "the ref description"} {
		if strings.Contains(combined, unwanted) {
			t.Errorf("the title call should not carry %q:\n%s", unwanted, combined)
		}
	}
	if strings.Contains(req.SystemPrompt, `"summary"`) {
		t.Errorf("the title-only call should not ask for a summary:\n%s", req.SystemPrompt)
	}
}

func TestDescriptionsStateTheTitleRule(t *testing.T) {
	entry, graph := titledFixture()
	for name, candidate := range map[string]*model.Entry{"title": untitledCopy(entry), "summary": unsummarizedCopy(entry)} {
		t.Run(name, func(t *testing.T) {
			description, err := llmops.NewDescription(candidate, graph, "", false)
			if err != nil {
				t.Fatalf("NewDescription: %v", err)
			}
			if !strings.Contains(description.Request.SystemPrompt, model.TitleRule) {
				t.Errorf("the prompt should state the title rule:\n%s", description.Request.SystemPrompt)
			}
			if strings.Contains(description.Request.SystemPrompt, "at most 32 characters") {
				t.Errorf("the prompt should no longer cap titles at 32 characters:\n%s", description.Request.SystemPrompt)
			}
		})
	}
}

// Each call constrains its answer to the fields its parser reads; the title-only
// call must not be asked for a summary it would discard.
func TestDescriptionsDeclareTheirSchema(t *testing.T) {
	entry, graph := titledFixture()
	for _, tc := range []struct {
		name    string
		entry   *model.Entry
		want    []string
		without string
	}{
		{name: "summary", entry: unsummarizedCopy(entry), want: []string{`"title"`, `"summary"`}},
		{name: "title", entry: untitledCopy(entry), want: []string{`"title"`}, without: `"summary"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			description, err := llmops.NewDescription(tc.entry, graph, "", false)
			if err != nil {
				t.Fatalf("NewDescription: %v", err)
			}
			schema := string(description.Request.ResponseSchema)
			for _, field := range tc.want {
				if !strings.Contains(schema, field) {
					t.Errorf("the request schema should declare %s, got %s", field, schema)
				}
			}
			if tc.without != "" && strings.Contains(schema, tc.without) {
				t.Errorf("the request schema should not declare %s, got %s", tc.without, schema)
			}
		})
	}
}

func TestPairedDescriptionParsesTheAnswer(t *testing.T) {
	entry, graph := titledFixture()
	description, err := llmops.NewDescription(unsummarizedCopy(entry), graph, "", false)
	if err != nil {
		t.Fatalf("NewDescription: %v", err)
	}
	cases := []struct {
		name        string
		output      string
		wantTitle   string
		wantSummary string
		wantErr     string
	}{
		{name: "paired answer", output: `{"title": "daemon project removal", "summary": "The daemon project is removed."}`, wantTitle: "Daemon project removal", wantSummary: "The daemon project is removed."},
		{name: "answer wrapped in prose", output: "Here it is:\n```json\n{\"title\": \"daemon project removal\", \"summary\": \"The daemon project is removed.\"}\n```", wantTitle: "Daemon project removal", wantSummary: "The daemon project is removed."},
		{name: "trailing period dropped from the title", output: `{"title": "daemon project removal.", "summary": "S."}`, wantTitle: "Daemon project removal", wantSummary: "S."},
		{name: "plain prose", output: "The daemon project is removed.", wantErr: "no JSON object"},
		{name: "empty title", output: `{"title": " ", "summary": "S."}`, wantErr: "empty title"},
		{name: "multi-line title", output: `{"title": "daemon\nremoval", "summary": "S."}`, wantErr: "more than one line"},
		{name: "empty summary", output: `{"title": "daemon project removal", "summary": ""}`, wantErr: "empty summary"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := description.Parse(tc.output)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("the answer should be refused naming its defect, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got.Title != tc.wantTitle || got.Summary != tc.wantSummary {
				t.Errorf("title and summary should be read from the answer, got %+v", got)
			}
		})
	}
}

func TestTitleDescriptionParsesTheAnswer(t *testing.T) {
	entry, graph := titledFixture()
	description, err := llmops.NewDescription(untitledCopy(entry), graph, "", false)
	if err != nil {
		t.Fatalf("NewDescription: %v", err)
	}
	got, err := description.Parse(`{"title": " daemon project removal "}`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Title != "Daemon project removal" || got.Summary != "" {
		t.Errorf("the title should be read trimmed and capitalized, leaving the summary alone, got %+v", got)
	}
	_, err = description.Parse(`{"title": ""}`)
	if err == nil {
		t.Error("an empty title should be refused")
	}
}

func TestDescribeGivesTitleAndSummaryFromOneCall(t *testing.T) {
	entry, graph := titledFixture()
	runner := &scriptedRunner{replies: []string{`{"title": "daemon project removal", "summary": "The daemon project is removed."}`}}
	got, err := llmops.Describe(context.Background(), runner, unsummarizedCopy(entry), graph, "", false)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if len(runner.requests) != 1 || got.Title == "" || got.Summary == "" {
		t.Errorf("an entry without a summary should get both from one call, got %+v after %d calls", got, len(runner.requests))
	}
}
