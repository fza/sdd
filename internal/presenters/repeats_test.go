package presenters_test

import (
	"bytes"
	"slices"

	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
	"github.com/networkteam/sdd/internal/query"
)

func heldOnly(ids ...string) *presenters.Repeats {
	return &presenters.Repeats{Held: func(id, _ string) bool { return slices.Contains(ids, id) }}
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
			presenters.RenderViewRepeats(&buf, &query.ViewResult{Graph: g, Sections: []query.SectionResult{tc.section}}, heldOnly(ids...))
			got := buf.String()
			for _, e := range tc.held {
				if !strings.Contains(got, heldPointer(e)) {
					t.Errorf("a held entry should print as its pointer:\n%s", got)
				}
			}
			if strings.Contains(got, other.ID) && (!strings.Contains(got, "Other entry.") || strings.Contains(got, heldPointer(other))) {
				t.Errorf("an entry not held should print its own line:\n%s", got)
			}
			if !strings.Contains(got, presenters.GatherWayBack) {
				t.Errorf("a serve printing a pointer should name the way back:\n%s", got)
			}
		})
	}
}

// heldPointer is the pointer an entry heldOnly holds prints as.
func heldPointer(e *model.Entry) string {
	return e.ID + " — " + model.EntryBrief(e) + " (served in full above)"
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
	presenters.RenderShow(&buf, result, presenters.ShowOptions{Repeats: heldOnly(basis.ID)})
	got := buf.String()
	if !strings.Contains(got, heldPointer(basis)) {
		t.Fatalf("a held chain entry should print as its pointer:\n%s", got)
	}
	if !strings.Contains(got, "# body") || !strings.Contains(got, "Decision on the basis.") {
		t.Fatalf("the primary not held should print in full:\n%s", got)
	}
}

// A held local hit prints as its pointer; a hit of a connected repo's graph
// keeps its line and citations.
func TestRenderSearch_HeldHitPrintsAsPointer(t *testing.T) {
	local := entry("20260101-100000-d-tac-hld", withSummary("Held entry."))
	remote := entry("20260101-110000-d-tac-rem", withSummary("Remote entry."))
	g := model.NewGraph([]*model.Entry{local})
	result := &query.SearchResult{Entries: []query.SearchEntry{
		{Entry: local, Citations: []query.Citation{{Snippet: "local-snippet", IsSummary: true, Score: 0.9}}},
		{Entry: remote, RepoID: "example.com/team/dep", Citations: []query.Citation{{Snippet: "remote-snippet", IsSummary: true, Score: 0.8}}},
	}}
	var buf bytes.Buffer
	presenters.RenderSearchRepeats(&buf, result, g, heldOnly(local.ID, remote.ID))
	got := buf.String()
	if !strings.Contains(got, local.ID+" — Held entry. (served in full above)") || strings.Contains(got, "local-snippet") {
		t.Errorf("a held local hit should print as its pointer without citations:\n%s", got)
	}
	if !strings.Contains(got, "remote-snippet") || strings.Contains(got, remote.ID+" — Remote entry. (served") {
		t.Errorf("a connected repo's hit should keep its line and citations:\n%s", got)
	}
}

// Every entry served in full is recorded with the body it prints; a held one
// is not.
func TestRenderShow_RecordsEntriesServedInFull(t *testing.T) {
	basis := entry("20260410-100000-s-stg-aaa", withSummary("Basis signal."))
	primary := entry("20260410-100200-d-tac-ccc", withContent("Decision on the basis."), withRefs(basis.ID))
	g := model.NewGraph([]*model.Entry{basis, primary})
	result, err := finders.New(finders.Options{}).OnGraph(g).Show(query.ShowQuery{IDs: []string{primary.ID, basis.ID}, UpDepth: 0, DownDepth: 0})
	if err != nil {
		t.Fatal(err)
	}
	served := map[string]string{}
	r := &presenters.Repeats{
		Held:   func(id, _ string) bool { return id == basis.ID },
		Served: func(id, _, body string) { served[id] = body },
	}
	var buf bytes.Buffer
	presenters.RenderShow(&buf, result, presenters.ShowOptions{Repeats: r})
	if !strings.Contains(served[primary.ID], "Decision on the basis.") {
		t.Errorf("the entry served in full should be recorded with its body, got %q", served[primary.ID])
	}
	if _, ok := served[basis.ID]; ok {
		t.Error("a held entry should not be recorded as served in full")
	}
}
