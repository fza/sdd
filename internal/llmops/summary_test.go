package llmops_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

func TestParseSummaryResponse(t *testing.T) {
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
			got, err := llmops.ParseSummaryResponse(tc.output)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("the answer should be refused naming its defect, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSummaryResponse: %v", err)
			}
			if got.Title != tc.wantTitle || got.Summary != tc.wantSummary {
				t.Errorf("title and summary should be read from the answer, got %+v", got)
			}
		})
	}
}

func TestParseTitleResponse(t *testing.T) {
	title, err := llmops.ParseTitleResponse(`{"title": " daemon project removal "}`)
	if err != nil {
		t.Fatalf("ParseTitleResponse: %v", err)
	}
	if title != "Daemon project removal" {
		t.Errorf("the title should be read trimmed and capitalized, got %q", title)
	}
	if _, err := llmops.ParseTitleResponse(`{"title": ""}`); err == nil {
		t.Error("an empty title should be refused")
	}
}

func titledFixture() (*model.Entry, *model.Graph) {
	related := &model.Entry{
		ID: "20260101-100000-s-tac-rel", Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical,
		Title: "daemon project removal", Summary: "The daemon project is slated for removal.", Content: "Remove it.",
	}
	entry := &model.Entry{
		ID: "20260102-100000-d-tac-ent", Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerTactical,
		Refs: []model.Ref{{ID: related.ID, Kind: model.RefKindAddresses}}, Summary: "A stored summary that may have been corrected.",
		Content: "The daemon project goes.",
	}
	return entry, model.NewGraph([]*model.Entry{related, entry})
}

func TestRenderSummaryPromptShowsRelatedTitles(t *testing.T) {
	entry, graph := titledFixture()
	req, err := llmops.RenderSummaryPrompt(entry, graph, "")
	if err != nil {
		t.Fatalf("RenderSummaryPrompt: %v", err)
	}
	if req.Purpose != llm.PurposeSummarize {
		t.Errorf("the paired call should carry the summarize purpose, got %q", req.Purpose)
	}
	if !strings.Contains(req.UserPrompt, "Title: daemon project removal") {
		t.Errorf("related entries should carry their titles, so a differing subject can be named apart:\n%s", req.UserPrompt)
	}
	if !strings.Contains(req.SystemPrompt, `{"title": "...", "summary": "..."}`) {
		t.Errorf("the prompt should ask for the paired answer:\n%s", req.SystemPrompt)
	}
}

func TestRenderTitlePromptCarriesTheStoredSummary(t *testing.T) {
	entry, graph := titledFixture()
	req, err := llmops.RenderTitlePrompt(entry, graph, "de")
	if err != nil {
		t.Fatalf("RenderTitlePrompt: %v", err)
	}
	if req.Purpose != llm.PurposeTitle {
		t.Errorf("the title-only call should carry the title purpose, got %q", req.Purpose)
	}
	if !strings.Contains(req.UserPrompt, entry.Summary) {
		t.Errorf("the stored summary should reach the title-only call:\n%s", req.UserPrompt)
	}
	if !strings.Contains(req.SystemPrompt, "`de`") {
		t.Errorf("the configured language should govern the title:\n%s", req.SystemPrompt)
	}
	if strings.Contains(req.SystemPrompt, `"summary"`) {
		t.Errorf("the title-only call should not ask for a summary:\n%s", req.SystemPrompt)
	}
}

func TestTitlePromptsStateTheTitleRule(t *testing.T) {
	renderers := map[string]func(*model.Entry, *model.Graph, string) (llm.Request, error){
		"title":   llmops.RenderTitlePrompt,
		"summary": llmops.RenderSummaryPrompt,
	}
	for name, render := range renderers {
		t.Run(name, func(t *testing.T) {
			entry, graph := titledFixture()
			req, err := render(entry, graph, "")
			if err != nil {
				t.Fatalf("rendering the %s prompt: %v", name, err)
			}
			if !strings.Contains(req.SystemPrompt, model.TitleRule) {
				t.Errorf("the prompt should state the title rule:\n%s", req.SystemPrompt)
			}
			if strings.Contains(req.SystemPrompt, "at most 32 characters") {
				t.Errorf("the prompt should no longer cap titles at 32 characters:\n%s", req.SystemPrompt)
			}
		})
	}
}
