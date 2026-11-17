package presenters_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// An entry a person approved in an assisted dialogue names who approved it and
// who stated its reason, wherever it is listed or shown.
func approvedGraph() (*model.Graph, *model.Entry) {
	e := entry("20260410-100000-d-cpt-apr", withIntent(model.IntentGuiding), withParticipants("Ada"), withSummary("Retry the flaky step once"))
	e.ApprovedBy, e.Reason, e.ReasonBy, e.IntentBy = "Ada", "It fails one run in ten.", model.DraftedByAgent, "Ada"
	return model.NewGraph([]*model.Entry{e}), e
}

func TestEntryLine_Approval(t *testing.T) {
	g, e := approvedGraph()
	if got, want := renderEntryLine(e, g), "  20260410-100000-d-cpt-apr conceptual directive decision [intent: guiding] [approved-by: Ada] [reason-by: agent] (Ada) {status: active} Retry the flaky step once\n"; got != want {
		t.Errorf("the line should name who approved and who gave the reason:\ngot  %q\nwant %q", got, want)
	}
	if got := renderBriefEntryLine(e, g); got != "  20260410-100000-d-cpt-apr conceptual directive decision [approved-by: Ada] [reason-by: agent] Retry the flaky step once\n" {
		t.Errorf("the brief line, as the catch-up lists it, should keep the approval, got %q", got)
	}
}

func TestRenderShow_Approval_Marks(t *testing.T) {
	g, e := approvedGraph()
	out := renderShow(t, g, []string{e.ID})
	for _, want := range []string{"approved-by: Ada\n", "reason-by: agent\n", "intent-by: Ada\n", "reason: It fails one run in ten.\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("show should carry %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "decided-by") || strings.Contains(out, "agent-mark") {
		t.Errorf("an approved entry carries no agent mark, got:\n%s", out)
	}
}
