package llmops_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/basefacts"
	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
)

// A delta check judges only the passages changed since the guide's last
// pass, with the whole draft as context (20261109-005108-d-cpt-wcj).
func TestWritingGuideDelta_ConfinesTheJudgmentToChangedPassages(t *testing.T) {
	graph := lightGraph(t)
	entry := &model.Entry{Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical, Content: "The loader drops the second option.\n\nIt happens on every reload."}
	facts := llmops.ReferenceFacts{Source: responseFactSource{graph}, TypeSystemFactID: basefacts.OverviewFactID}

	runner := &lightRunner{}
	_, err := llmops.WritingGuideDelta(t.Context(), runner, entry, nil, facts, []string{"It happens on every reload."})
	if err != nil {
		t.Fatal(err)
	}
	prompt := runner.prompts[0].UserPrompt
	if !strings.Contains(prompt, "## Changed since the last check") || !strings.Contains(prompt, "````\nIt happens on every reload.\n````") || !strings.Contains(prompt, "The loader drops the second option.") {
		t.Fatalf("a delta check should carry the whole draft and the changed passage:\n%s", prompt)
	}

	full := &lightRunner{}
	_, err = llmops.WritingGuide(t.Context(), full, entry, nil, facts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(full.prompts[0].UserPrompt, "Changed since the last check") {
		t.Fatal("a full check should judge the whole draft")
	}
}
