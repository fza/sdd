package model_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// An entry a person approved in an assisted dialogue is the person's: it
// carries approved-by, and reason-by and intent-by name who stated its reason
// and intent, the person or the agent that drafted them.

func approvedEntry(id string, refs ...model.Ref) *model.Entry {
	e := personEntry(id, refs...)
	e.ApprovedBy = "Ada"
	return e
}

func TestApprovedEntryCutsTheChain(t *testing.T) {
	const (
		a = "20260101-100000-s-tac-aaa"
		b = "20260101-100100-s-tac-bbb"
		c = "20260101-100200-s-tac-ccc"
	)
	// b also carries decided-by, as a hand edit might leave it: approved-by wins.
	marked := approvedEntry(b, ref(a, model.RefKindGroundedIn))
	marked.DecidedBy = model.DecidedByAgent
	g := model.NewGraph([]*model.Entry{agentEntry(a), marked, agentEntry(c, ref(b, model.RefKindGroundedIn))})
	if marked.AgentDecided() {
		t.Error("an approved entry should not read as an agent's alone")
	}
	got := g.AgentMarks()
	if _, ok := got[b]; ok {
		t.Errorf("an approved entry should carry no mark, got %+v", got[b])
	}
	if mark := got[c]; !mark.Marked || mark.Hops != 0 || !slices.Equal(mark.Roots, []string{c}) {
		t.Errorf("an agent's entry built on an approved one should start its own chain, got %+v", mark)
	}
}

func TestApprovedAuthorshipConstruction(t *testing.T) {
	g := model.NewGraph(nil)
	directive := func(edit func(*model.Entry)) func() *model.Entry {
		return func() *model.Entry {
			e := &model.Entry{ID: "20260101-100000-d-cpt-new", Type: model.TypeDecision, Layer: model.LayerConceptual, Kind: model.KindDirective,
				Intent: model.IntentGuiding, Confidence: "high", Content: "Body.", ApprovedBy: "Ada", Reason: "It keeps the build green.", ReasonBy: "agent", IntentBy: "Ada"}
			edit(e)
			return e
		}
	}
	cases := []struct {
		name    string
		entry   func() *model.Entry
		finding string
	}{
		{"an approved directive with its reason and intent attributed", directive(func(*model.Entry) {}), ""},
		{"an approved directive the agent also decided", directive(func(e *model.Entry) { e.DecidedBy = model.DecidedByAgent }), "approved-by"},
		{"an approved plan without a reason", directive(func(e *model.Entry) {
			e.Kind, e.Intent, e.IntentBy, e.Reason, e.ReasonBy = model.KindPlan, "", "", "", ""
			e.Content = "Body.\n\n## Acceptance criteria\n\n- [ ] It holds."
		}), ""},
		{"an approved activity without a reason", directive(func(e *model.Entry) {
			e.Kind, e.Intent, e.IntentBy, e.Reason, e.ReasonBy = model.KindActivity, "", "", "", ""
		}), ""},
		{"an approved directive without a reason", directive(func(e *model.Entry) { e.Reason, e.ReasonBy = "", "" }), ""},
		{"an approved decision whose reason names no one", directive(func(e *model.Entry) { e.ReasonBy = "" }), "reason-by"},
		{"an approved directive whose intent names no one", directive(func(e *model.Entry) { e.IntentBy = "" }), "intent-by"},
		{"reason-by on an entry no one approved", directive(func(e *model.Entry) { e.ApprovedBy, e.IntentBy = "", "" }), "reason-by"},
		{"intent-by on an approved plan", directive(func(e *model.Entry) { e.Kind, e.Intent = model.KindPlan, "" }), "intent-by"},
		{"an approved signal", func() *model.Entry {
			return &model.Entry{ID: "20260101-100000-s-tac-new", Type: model.TypeSignal, Layer: model.LayerTactical, Kind: model.KindGap, Confidence: "high", Content: "Body.", ApprovedBy: "Ada"}
		}, ""},
		{"an approved signal with a reason", func() *model.Entry {
			return &model.Entry{ID: "20260101-100000-s-tac-new", Type: model.TypeSignal, Layer: model.LayerTactical, Kind: model.KindGap, Confidence: "high", Content: "Body.", ApprovedBy: "Ada", Reason: "Why.", ReasonBy: "Ada"}
		}, "reason"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			construction, findings := model.ConstructFromEntry(tc.entry())
			_, writeFindings := construction.ValidateForWrite(g)
			var fields []string
			for _, f := range append(findings, writeFindings...) {
				fields = append(fields, f.Field)
			}
			if tc.finding == "" && len(fields) > 0 {
				t.Errorf("findings on %v, want none", fields)
			}
			if tc.finding != "" && !slices.Contains(fields, tc.finding) {
				t.Errorf("findings on %v, want one on %s", fields, tc.finding)
			}
		})
	}
}

func TestApprovedAuthorshipRoundTrip(t *testing.T) {
	e := &model.Entry{
		ID: "20260101-100000-d-cpt-new", Type: model.TypeDecision, Layer: model.LayerConceptual, Kind: model.KindDirective, Intent: model.IntentGuiding,
		Participants: []string{"Ada"}, ApprovedBy: "Ada", Reason: "It keeps the build green.", ReasonBy: "Ada", IntentBy: "agent", Content: "Body.",
	}
	text := model.FormatFrontmatter(e) + "\n" + e.Content + "\n"
	for _, line := range []string{"approved-by: Ada", "reason-by: Ada", "intent-by: agent", "reason: It keeps the build green."} {
		if !strings.Contains(text, line) {
			t.Errorf("frontmatter lacks %q:\n%s", line, text)
		}
	}
	parsed, err := model.ParseEntry("20260101-100000-d-cpt-new.md", text)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.ApprovedBy != "Ada" || parsed.ReasonBy != "Ada" || parsed.IntentBy != "agent" {
		t.Errorf("parsed approval = %q, %q, %q", parsed.ApprovedBy, parsed.ReasonBy, parsed.IntentBy)
	}
	construction, _ := model.ConstructFromEntry(parsed)
	if back := construction.Entry(); back.ApprovedBy != e.ApprovedBy || back.ReasonBy != e.ReasonBy || back.IntentBy != e.IntentBy {
		t.Errorf("construction drops the approval: %q, %q, %q", back.ApprovedBy, back.ReasonBy, back.IntentBy)
	}
}
