package llmops_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
)

// The deep verifier's reading states who decided an entry when an agent did,
// and the unapproved agent reasoning it rests on.
func TestDeepReadingHeaderStatesTheAgentMark(t *testing.T) {
	root := readingEntry("20261101-090000-s-tac-agt", "An agent's observation.", "Body.")
	root.DecidedBy, root.RunBy = model.DecidedByAgent, "Ada"
	child := readingEntry("20261101-100000-s-tac-chd", "Built on it.", "Body.")
	child.DecidedBy, child.RunBy = model.DecidedByAgent, "Ada"
	child.Refs = []model.Ref{{ID: root.ID, Kind: model.RefKindGroundedIn}}
	person := readingEntry("20261101-110000-s-tac-per", "A person's reading.", "Body.")
	person.Refs = []model.Ref{{ID: child.ID, Kind: model.RefKindGroundedIn}}
	graph := lightGraph(t, root, child, person)

	if got := llmops.DeepReadingHeader(graph, root, model.LightRoleBrief, ""); !strings.Contains(got, "Decided by: an agent alone, run by Ada\n") || strings.Contains(got, "Agent-reasoned") {
		t.Errorf("an agent's root should name the agent and no chain, got:\n%s", got)
	}
	if got := llmops.DeepReadingHeader(graph, child, model.LightRoleBrief, ""); !strings.Contains(got, "Agent-reasoned via s-tac-agt, 1 step(s) back") {
		t.Errorf("an entry on the chain should name its root, got:\n%s", got)
	}
	if got := llmops.DeepReadingHeader(graph, person, model.LightRoleBrief, ""); strings.Contains(got, "agent") {
		t.Errorf("a person's entry should carry no mark, got:\n%s", got)
	}
}

// Pre-flight and the summary read an approval's approves list and an agent's
// mark off the entry as the prompt formats it.
func TestFormatEntryForPromptCarriesTheMark(t *testing.T) {
	e := &model.Entry{ID: "20261101-090000-d-tac-agt", Type: model.TypeDecision, Layer: model.LayerTactical, Kind: model.KindPlan,
		DecidedBy: model.DecidedByAgent, RunBy: "Ada", Reason: "The build needs it.", Approves: []string{"20261101-080000-d-tac-old"}}
	got := llmops.FormatEntryForPrompt(e)
	for _, want := range []string{"Approves: 20261101-080000-d-tac-old\n", "Decided by: an agent alone, run by Ada\n", "Agent's reason: The build needs it.\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("the prompt form should carry %q, got:\n%s", want, got)
		}
	}
}
