package llmops_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
)

// The deep verifier's reading and the prompt form of an entry state who
// approved it and who stated its reason and intent.
func TestReadingsStateTheApproval(t *testing.T) {
	e := readingEntry("20261101-090000-d-cpt-apr", "A person's approved directive.", "Body.")
	e.Kind, e.Intent = model.KindDirective, model.IntentGuiding
	e.ApprovedBy, e.Reason, e.ReasonBy, e.IntentBy = "Ada", "It keeps the build green.", model.DraftedByAgent, "Ada"
	graph := lightGraph(t, e)
	want := []string{"Approved by: Ada, drafted by an agent\n", "Reason, drafted by the agent: It keeps the build green.\n", "Intent set by: Ada\n"}

	header := llmops.DeepReadingHeader(graph, e, model.LightRoleBrief, "")
	prompt := llmops.FormatEntryForPrompt(e)
	for _, line := range want {
		if !strings.Contains(header, line) {
			t.Errorf("the deep reading should state %q, got:\n%s", line, header)
		}
		if !strings.Contains(prompt, line) {
			t.Errorf("the prompt form should state %q, got:\n%s", line, prompt)
		}
	}
	if strings.Contains(prompt, "Agent's reason") || strings.Contains(header, "Decided by") {
		t.Errorf("an approved entry's reason is not an agent's decision, got:\n%s\n%s", header, prompt)
	}

	e.ReasonBy = "Ada"
	if got := llmops.FormatEntryForPrompt(e); !strings.Contains(got, "Reason, given by Ada: It keeps the build green.\n") {
		t.Errorf("a reason the person gave should be theirs, got:\n%s", got)
	}
}
