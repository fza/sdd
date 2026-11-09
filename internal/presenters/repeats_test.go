package presenters_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/pkg/application/types"
)

func heldOnly(ids ...string) *presenters.Repeats {
	return &presenters.Repeats{Held: func(id string) bool {
		for _, held := range ids {
			if held == id {
				return true
			}
		}
		return false
	}}
}

// Every view renderer prints a held entry as its pointer and keeps the rest.
func TestRenderView_HeldEntryPrintsAsPointer(t *testing.T) {
	held := entry("20260101-100000-d-tac-hld", withKind(model.KindDirective), withParticipants("Christopher"), withSummary("Held entry."))
	other := entry("20260101-110000-d-tac-oth", withKind(model.KindDirective), withParticipants("Christopher"), withSummary("Other entry."))
	focus := entry("20260101-120000-d-prc-foc", withKind(model.KindFocus), withParticipants("Christopher"), withSummary("Held focus."))
	actor := entry("20260424-100000-s-prc-act", withKind(model.KindActor), withCanonical("Christopher"), withParticipants("Christopher"), withSummary("Held actor."))
	role := entry("20260424-110000-d-prc-rol", withKind(model.KindRole), withActor("Christopher"), withParticipants("Christopher"), withSummary("Held role."))
	g := model.NewGraph([]*model.Entry{held, other, focus, actor, role})

	cases := []struct {
		name    string
		section query.SectionResult
		held    []*model.Entry
	}{
		{"as-grouped", query.SectionResult{Render: "as-grouped", Data: model.Grouped{Field: "kind", Groups: []model.Group{{Key: "directive", Entries: []*model.Entry{held, other}}}}}, []*model.Entry{held}},
		{"as-focus-block header", query.SectionResult{Render: "as-focus-block", Data: model.FocusBlock{Focuses: []model.FocusGroup{{Focus: focus, Targets: []model.FocusTarget{{Target: other, State: model.FocusStateDriving}}}}}}, []*model.Entry{focus}},
		{"as-focus-block target", query.SectionResult{Render: "as-focus-block", Data: model.FocusBlock{Focuses: []model.FocusGroup{{Focus: focus, Targets: []model.FocusTarget{{Target: held, State: model.FocusStateDriving}, {Target: other, State: model.FocusStateDriving}}}}}}, []*model.Entry{held}},
		{"as-participants-block", query.SectionResult{Render: "as-participants-block", Data: model.ParticipantsBlock{Groups: []model.ParticipantsGroup{{Actor: actor, Roles: []*model.Entry{role}}}}}, []*model.Entry{actor, role}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ids []string
			for _, e := range tc.held {
				ids = append(ids, e.ID)
			}
			var buf bytes.Buffer
			sections := (&query.ViewResult{Graph: g, Sections: []query.SectionResult{tc.section}}).ViewSections("")
			presenters.RenderView(&buf, sections, heldOnly(ids...))
			got := buf.String()
			for _, e := range tc.held {
				if !strings.Contains(got, presenters.GatherPointer(e)) {
					t.Errorf("a held entry should print as its pointer:\n%s", got)
				}
			}
			if strings.Contains(got, other.ID) && (!strings.Contains(got, "Other entry.") || strings.Contains(got, presenters.GatherPointer(other))) {
				t.Errorf("an entry not held should print its own line:\n%s", got)
			}
			if !strings.Contains(got, presenters.GatherWayBack) {
				t.Errorf("a serve printing a pointer should name the way back:\n%s", got)
			}
		})
	}
}

// A held entry on a show chain prints as its pointer on the bullet.
func TestRenderShow_HeldChainEntryPrintsAsPointer(t *testing.T) {
	basis := entry("20260410-100000-s-stg-aaa", withSummary("Basis signal."))
	primary := entry("20260410-100200-d-tac-ccc", withContent("Decision on the basis."), withRefs(basis.ID))
	g := model.NewGraph([]*model.Entry{basis, primary})
	result, err := finders.New(finders.Options{}).OnGraph(g).Show(query.ShowQuery{IDs: []string{primary.ID}, UpDepth: query.DefaultUpDepth, DownDepth: query.DefaultDownDepth})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	entries, err := result.ShownEntries()
	if err != nil {
		t.Fatal(err)
	}
	if err := presenters.RenderShow(&buf, entries, presenters.ShowOptions{Repeats: heldOnly(basis.ID)}); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, presenters.GatherPointer(basis)) {
		t.Fatalf("a held chain entry should print as its pointer:\n%s", got)
	}
	if !strings.Contains(got, "# body") {
		t.Fatalf("the primary not held should print in full:\n%s", got)
	}
}

// A held local hit prints as its pointer; a hit without a brief, one of a
// connected repo's graph, keeps its line and citations.
func TestRenderSearch_HeldHitPrintsAsPointer(t *testing.T) {
	local := types.SearchHit{ID: "20260101-100000-d-tac-hld", Layer: "tactical", Type: "decision", Brief: "Held entry.",
		Citations: []types.SearchCitation{{Snippet: "local-snippet", IsSummary: true, Score: 0.9}}}
	remote := types.SearchHit{ID: "example.com/team/dep:20260101-110000-d-tac-rem", Layer: "tactical", Type: "decision",
		Citations: []types.SearchCitation{{Snippet: "remote-snippet", IsSummary: true, Score: 0.8}}}
	var buf bytes.Buffer
	presenters.RenderSearch(&buf, []types.SearchHit{local, remote}, heldOnly(local.ID, remote.ID))
	got := buf.String()
	if !strings.Contains(got, local.ID+" — Held entry. (served in full above)") || strings.Contains(got, "local-snippet") {
		t.Errorf("a held local hit should print as its pointer without citations:\n%s", got)
	}
	if !strings.Contains(got, "remote-snippet") || strings.Contains(got, remote.ID+" — ") {
		t.Errorf("a hit without a brief should keep its line and citations:\n%s", got)
	}
}
