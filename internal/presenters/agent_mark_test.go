package presenters_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
)

// agentChainGraph holds an agent's directive, a person's plan, and two agent
// entries built on the directive, the second through the first.
func agentChainGraph() (*model.Graph, *model.Entry, *model.Entry, *model.Entry) {
	root := entry("20260410-100000-d-tac-agt", withIntent(model.IntentPending), withParticipants("Claude"), withSummary("Retry the flaky step once"))
	root.DecidedBy, root.RunBy, root.Reason = model.DecidedByAgent, "Ada", "It fails one run in ten."
	first := entry("20260410-110000-d-tac-one", withKind(model.KindPlan), withParticipants("Claude"), withSummary("Plan the retry"))
	first.DecidedBy, first.RunBy, first.Reason = model.DecidedByAgent, "Ada", "The directive needs a plan."
	first.Refs = []model.Ref{{ID: root.ID, Kind: model.RefKindAddresses}, {ID: root.ID, Kind: model.RefKindGroundedIn}}
	second := entry("20260410-120000-s-tac-two", withParticipants("Claude"), withSummary("The retry needs a timeout"))
	second.DecidedBy, second.RunBy = model.DecidedByAgent, "Ada"
	second.Refs = []model.Ref{{ID: first.ID, Kind: model.RefKindBuildsOn}}
	person := entry("20260410-130000-d-tac-per", withKind(model.KindPlan), withParticipants("Ada"), withSummary("A person's plan"))
	person.Refs = []model.Ref{{ID: second.ID, Kind: model.RefKindGroundedIn}}
	return model.NewGraph([]*model.Entry{root, first, second, person}), root, second, person
}

func TestEntryLine_AgentMark(t *testing.T) {
	g, root, second, person := agentChainGraph()
	if got, want := renderEntryLine(root, g), "  20260410-100000-d-tac-agt tactical directive decision [decided-by: agent] [run-by: Ada] (Claude) {status: active} Retry the flaky step once\n"; got != want {
		t.Errorf("an agent's root should carry its stored mark and no chain:\ngot  %q\nwant %q", got, want)
	}
	if got := renderEntryLine(second, g); !strings.Contains(got, "[decided-by: agent] [run-by: Ada] (Claude) {status: open} {agent-reasoned via d-tac-agt, 2 steps}") {
		t.Errorf("an entry two steps down the chain should name its root, got %q", got)
	}
	if got := renderEntryLine(person, g); strings.Contains(got, "agent") {
		t.Errorf("a person's entry built on the chain should carry no mark, got %q", got)
	}
	if got := renderBriefEntryLine(second, g); got != "  20260410-120000-s-tac-two tactical gap signal [decided-by: agent] The retry needs a timeout\n" {
		t.Errorf("the brief line should keep the agent's authorship, got %q", got)
	}
}

func TestRenderShow_AgentMark(t *testing.T) {
	g, _, second, _ := agentChainGraph()
	out := renderShow(t, g, []string{second.ID})
	for _, want := range []string{"decided-by: agent\n", "run-by: Ada\n", "agent-mark: agent-reasoned via d-tac-agt, 2 steps\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("show should carry %q, got:\n%s", want, out)
		}
	}
	root := renderShow(t, g, []string{"20260410-100000-d-tac-agt"})
	if !strings.Contains(root, "reason: It fails one run in ten.\n") || strings.Contains(root, "agent-mark:") {
		t.Errorf("show of the root should carry the agent's reason and no chain mark, got:\n%s", root)
	}
}

func TestRenderShow_Approval(t *testing.T) {
	g, root, _, _ := agentChainGraph()
	approval := entry("20260410-140000-d-tac-apr", withIntent(model.IntentSettled), withParticipants("Ada"), withSummary("The retry holds"))
	approval.Approves = []string{root.ID}
	g = model.NewGraph(append(g.Entries, approval))
	if out := renderShow(t, g, []string{approval.ID}); !strings.Contains(out, "approves:\n    - "+root.ID) || !strings.Contains(out, "- approves "+root.ID) {
		t.Errorf("show of an approval should list and link what it approves, got:\n%s", out)
	}
	if out := renderShow(t, g, []string{root.ID}); !strings.Contains(out, "- approved-by "+approval.ID) || strings.Contains(out, "agent-mark:") {
		t.Errorf("show of an approved entry should link its approval, got:\n%s", out)
	}
}

func TestRenderAgentChains(t *testing.T) {
	g, root, second, _ := agentChainGraph()
	var buf bytes.Buffer
	presenters.RenderAgentChains(&buf, g.AgentChains())
	out := buf.String()
	for _, want := range []string{
		"## Agent-decided, awaiting review (1)",
		"- " + root.ID + " tactical directive: Retry the flaky step once",
		"  - rests on it: " + second.ID + " tactical gap: The retry needs a timeout",
		"approves",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the lane should carry %q, got:\n%s", want, out)
		}
	}
	buf.Reset()
	presenters.RenderAgentChains(&buf, nil)
	if buf.Len() != 0 {
		t.Errorf("no chain should render nothing, got %q", buf.String())
	}
}
