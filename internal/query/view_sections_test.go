package query_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/pkg/application/types"
)

func TestViewResult_ViewSectionsCarriesEveryShape(t *testing.T) {
	plan := &model.Entry{
		ID: "20261101-100000-d-tac-pln", Type: model.TypeDecision, Layer: model.LayerTactical, Kind: model.KindPlan,
		Title: "Ship sections", Confidence: "high", Intent: model.IntentGuiding, Participants: []string{"Alice"},
		Topics: []model.TopicPath{topicPath(t, "cli/view")}, Summary: "Ship it.", Content: "Ship the sections.\nIn full.",
	}
	gap := &model.Entry{
		ID: "20261101-110000-s-tac-gap", Type: model.TypeSignal, Layer: model.LayerTactical, Kind: model.KindGap,
		Content: "The view has no data form. Callers parse text.",
	}
	done := &model.Entry{
		ID: "20261101-120000-s-tac-don", Type: model.TypeSignal, Layer: model.LayerTactical, Kind: model.KindDone,
		Title: "Sections landed", Closes: []string{gap.ID}, Content: "Landed.",
	}
	actor := &model.Entry{
		ID: "20261101-090000-s-prc-act", Type: model.TypeSignal, Layer: model.LayerProcess, Kind: model.KindActor,
		Title: "Alice", Canonical: "Alice", Content: "Alice works here.",
	}
	focus := &model.Entry{
		ID: "20261101-080000-d-tac-fcs", Type: model.TypeDecision, Layer: model.LayerTactical, Kind: model.KindFocus,
		Title: "View work", Content: "Focus on the view.",
	}
	graph := model.NewGraph([]*model.Entry{plan, gap, done, actor, focus})

	planListed := types.ListedEntry{
		ID: plan.ID, Title: "Ship sections", ListingTitle: "Ship sections", Type: "decision", Kind: "plan", Layer: "tactical",
		Status: "active", DerivedStatus: types.DerivedStatus{Kind: "active"}, Confidence: "high", Intent: "guiding",
		Participants: []string{"Alice"}, Topics: []string{"cli/view"}, Summary: "Ship it.", BodyExcerpt: "Ship the sections.", Brief: "Ship sections",
	}
	gapListed := types.ListedEntry{
		ID: gap.ID, ListingTitle: "The view has no data form.", Type: "signal", Kind: "gap", Layer: "tactical",
		Status: "closed-by " + done.ID, DerivedStatus: types.DerivedStatus{Kind: "closed-by", By: done.ID},
		BodyExcerpt: "The view has no data form. Callers parse text.", Brief: "The view has no data form.",
	}
	actorListed := types.ListedEntry{
		ID: actor.ID, Title: "Alice", ListingTitle: "Alice", Type: "signal", Kind: "actor", Layer: "process",
		Status: "open", DerivedStatus: types.DerivedStatus{Kind: "open"}, BodyExcerpt: "Alice works here.", Canonical: "Alice", Brief: "Alice",
	}
	focusListed := types.ListedEntry{
		ID: focus.ID, Title: "View work", ListingTitle: "View work", Type: "decision", Kind: "focus", Layer: "tactical",
		Status: "active", DerivedStatus: types.DerivedStatus{Kind: "active"}, BodyExcerpt: "Focus on the view.", Brief: "View work",
	}
	planBody := planListed
	planBody.Body = plan.Content
	markedAt := time.Date(2026, 11, 1, 19, 45, 2, 0, time.UTC)

	cases := []struct {
		name    string
		repo    model.RepoID
		section query.SectionResult
		want    types.ViewSection
	}{
		{
			name: "ranked list with expanded refs",
			section: query.SectionResult{Render: "as-list", Name: "Hot", Brief: true, Data: model.FlatList{
				Entries: []*model.Entry{plan, gap}, Scores: []float64{2.5, 1},
				RefExpansions: [][]model.RefExpansion{
					{{
						Kind: model.RefKindBuildsOn, ID: "20261101-070000-d-tac-old", Desc: "the base",
						Status:        model.Status{Kind: model.StatusSupersededBy, By: "20261101-075000-d-tac-hed"},
						SupersedePath: []string{"20261101-070000-d-tac-old", "20261101-072000-d-tac-mid", "20261101-075000-d-tac-hed"},
					}},
					{{Kind: model.RefKindUnknown, ID: "20261101-060000-s-tac-far", UnresolvedRepo: "example.com/team/remote"}},
				},
				RefExpansionDropped: []int{3, 0},
			}},
			want: types.ViewSection{Render: "as-list", Name: "Hot", Brief: true, Matched: 2, List: &types.ViewList{
				Ranked: true, RefsExpanded: true,
				Items: []types.ViewListItem{
					{Entry: planListed, Score: 2.5, RefsDropped: 3, Refs: []types.ViewRef{{
						Kind: "builds-on", ID: "20261101-070000-d-tac-old", Desc: "the base",
						DerivedStatus: types.DerivedStatus{
							Kind: "superseded-by", By: "20261101-075000-d-tac-hed",
							SupersedePath: []string{"20261101-070000-d-tac-old", "20261101-072000-d-tac-mid", "20261101-075000-d-tac-hed"},
						},
					}}},
					{Entry: gapListed, Score: 1, Refs: []types.ViewRef{{Kind: "unknown", ID: "20261101-060000-s-tac-far", UnresolvedRepo: "example.com/team/remote"}}},
				},
			}},
		},
		{
			name: "list ignores scores and expansions not parallel to its entries",
			section: query.SectionResult{Render: "as-list", Data: model.FlatList{
				Entries: []*model.Entry{plan}, Scores: []float64{2.5, 1}, RefExpansions: [][]model.RefExpansion{{}, {}},
			}},
			want: types.ViewSection{Render: "as-list", Matched: 1, List: &types.ViewList{Items: []types.ViewListItem{{Entry: planListed}}}},
		},
		{
			name: "grouped",
			section: query.SectionResult{Render: "as-grouped", Data: model.Grouped{Field: "kind", Groups: []model.Group{
				{Key: "gap", Entries: []*model.Entry{gap}},
				{Key: "plan", Entries: []*model.Entry{plan}},
			}}},
			want: types.ViewSection{Render: "as-grouped", Matched: 2, Grouped: &types.ViewGrouped{Field: "kind", Groups: []types.ViewGroup{
				{Key: "gap", Entries: []types.ListedEntry{gapListed}},
				{Key: "plan", Entries: []types.ListedEntry{planListed}},
			}}},
		},
		{
			name: "counts",
			section: query.SectionResult{Render: "as-counts", Data: model.Counts{
				Rows: []model.CountRow{{Label: "cli/view", Count: 2, Heat: 1.5}, {Label: "portability/mcp", Count: 1, Heat: 0.25}}, Matched: 3,
			}},
			want: types.ViewSection{Render: "as-counts", Matched: 3, Counts: &types.ViewCounts{Rows: []types.ViewCountRow{
				{Label: "cli/view", Count: 2, Heat: 1.5}, {Label: "portability/mcp", Count: 1, Heat: 0.25},
			}}},
		},
		{
			name: "focus block drops unresolved targets",
			section: query.SectionResult{Render: "as-focus-block", Data: model.FocusBlock{
				Focuses: []model.FocusGroup{
					{
						Focus: focus, Actors: []string{"Alice"}, When: &model.FocusWhen{From: "2026-11-01"},
						Targets: []model.FocusTarget{
							{
								Target: plan, ResolvedActors: []string{"Alice"}, ActorsExplicit: true,
								ResolvedWhen: &model.FocusWhen{To: "2026-11-30"}, Score: 0.5, State: model.FocusStateDriving,
							},
							{Target: nil, State: model.FocusStateStalled},
							{Target: gap, State: model.FocusStatePullAvailable},
						},
					},
					{Focus: nil},
				},
				Dropped: 2, Pull: "focus",
			}},
			want: types.ViewSection{Render: "as-focus-block", Matched: 2, Focus: &types.ViewFocusBlock{
				Focuses: []types.ViewFocus{
					{
						Focus: &focusListed, Actors: []string{"Alice"}, When: &types.FocusWhen{From: "2026-11-01"},
						Targets: []types.ViewFocusTarget{
							{
								Target: planListed, Actors: []string{"Alice"}, ActorsExplicit: true,
								When: &types.FocusWhen{To: "2026-11-30"}, Score: 0.5, State: "driving",
							},
							{Target: gapListed, State: "pull-available"},
						},
					},
					{},
				},
				Dropped: 2, Pull: "focus",
			}},
		},
		{
			name: "participants block",
			section: query.SectionResult{Render: "as-participants-block", Brief: true, Data: model.ParticipantsBlock{
				Groups:  []model.ParticipantsGroup{{Actor: actor, Roles: []*model.Entry{plan}}, {Actor: nil}},
				Dropped: 1, Pull: "participants:brief",
			}},
			want: types.ViewSection{Render: "as-participants-block", Brief: true, Matched: 2, Participants: &types.ViewParticipantsBlock{
				Groups:  []types.ViewParticipantsGroup{{Actor: &actorListed, Roles: []types.ListedEntry{planListed}}, {}},
				Dropped: 1, Pull: "participants:brief",
			}},
		},
		{
			name: "wip list",
			section: query.SectionResult{Render: "as-wip-list", Name: "WIP", Data: model.WipList{
				Markers: []*model.WIPMarker{{
					ID: "20261101-194502-alice", Entry: plan.ID, Participant: "Alice", Exclusive: true, Branch: "pr/feat-view-sections",
					Content: "implementing the sections\nthen the MCP objects", Time: markedAt,
				}},
				Dropped: 4, Pull: "wip",
			}},
			want: types.ViewSection{Render: "as-wip-list", Name: "WIP", Matched: 1, Wip: &types.ViewWipList{
				Markers: []types.ViewWipMarker{{
					ID: "20261101-194502-alice", Entry: plan.ID, Participant: "Alice", Exclusive: true, Branch: "pr/feat-view-sections",
					Content: "implementing the sections\nthen the MCP objects", ContentExcerpt: "implementing the sections", Time: markedAt,
				}},
				Dropped: 4, Pull: "wip",
			}},
		},
		{
			name: "bodies carry the full body, tagged with their repo",
			repo: "example.com/team/dep",
			section: query.SectionResult{Render: "as-bodies", Name: "Principles", Data: model.Bodies{
				Entries: []*model.Entry{plan}, Dropped: 5, Pull: "active:as-bodies",
			}},
			want: types.ViewSection{Render: "as-bodies", Name: "Principles", Repo: "example.com/team/dep", Matched: 1, Bodies: &types.ViewBodies{
				Entries: []types.ListedEntry{planBody}, Dropped: 5, Pull: "active:as-bodies",
			}},
		},
		{
			name:    "a section without data fills no shape",
			section: query.SectionResult{Render: "as-list"},
			want:    types.ViewSection{Render: "as-list"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := &query.ViewResult{Graph: graph, Sections: []query.SectionResult{tc.section}}
			got := result.ViewSections(tc.repo)
			if !reflect.DeepEqual(got, []types.ViewSection{tc.want}) {
				t.Errorf("the public section should carry everything the section holds:\n got: %#v\nwant: %#v", got, []types.ViewSection{tc.want})
			}
		})
	}
}
