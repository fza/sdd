package query_test

import (
	"reflect"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/pkg/application/types"
)

func yamlNode(t *testing.T, text string) yaml.Node {
	t.Helper()
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(text), &document); err != nil {
		t.Fatal(err)
	}
	return *document.Content[0]
}

func topicPath(t *testing.T, label string) model.TopicPath {
	t.Helper()
	path, err := model.ParseTopicPath(label)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestShowResult_ShownEntriesCarriesTheWholePrimary(t *testing.T) {
	index, err := model.NewFactIndex("Composing views", "cli/view")
	if err != nil {
		t.Fatal(err)
	}
	captured := time.Date(2026, 11, 1, 18, 23, 33, 0, time.UTC)
	primary := &model.Entry{
		ID: "20261101-182333-d-tac-jiy", Type: model.TypeDecision, Layer: model.LayerTactical, Kind: model.KindProcedure,
		Title: "Shared renderers", Confidence: "high", Intent: model.IntentGuiding, Participants: []string{"Christopher"},
		Canonical: "render", Aliases: []string{"renderer"}, ActorKind: model.ActorKindHuman, Class: "move", Actor: "christopher",
		Index: index, Refs: []model.Ref{{ID: "20261101-173830-s-tac-hnv", Kind: model.RefKindBuildsOn, Desc: "the partial types"}},
		Closes: []string{"20261101-170000-s-tac-gap"}, Supersedes: []string{"20261101-160000-d-tac-old"},
		Attachments: []string{"notes.md"}, Preflight: "bypassed", Time: captured, Summary: "Render once.", Content: "One renderer.",
		ProcedureSpec: &model.ProcedureSpecRaw{Params: yamlNode(t, "mode: fast\nlimit: 3\n"), Steps: yamlNode(t, "- id: first\n")},
	}
	result := &query.ShowResult{Groups: []query.ShowGroup{
		{},
		{
			Primary: primary, PrimaryID: "example.com/team/remote:" + primary.ID,
			PrimaryStatus:        model.Status{Kind: model.StatusSupersededBy, By: "20261101-190000-d-tac-hed"},
			PrimarySupersedePath: []string{primary.ID, "20261101-185000-d-tac-mid", "20261101-190000-d-tac-hed"},
			PrimaryTopics:        []model.TopicPath{topicPath(t, "cli/view"), topicPath(t, "portability/mcp")},
			UpstreamTruncated:    []model.TruncatedRef{{ID: "20261101-150000-s-tac-cut"}},
		},
	}}

	shown, err := result.ShownEntries()
	if err != nil {
		t.Fatal(err)
	}
	want := []types.ShownEntry{{
		ID: "example.com/team/remote:" + primary.ID, Title: "Shared renderers", Type: "decision", Kind: "procedure", Layer: "tactical",
		Status: "superseded-by 20261101-190000-d-tac-hed", Summary: "Render once.", Body: "One renderer.",
		UpstreamTruncated: []string{"20261101-150000-s-tac-cut"},
		Confidence:        "high", Intent: "guiding", Participants: []string{"Christopher"}, Canonical: "render", Aliases: []string{"renderer"},
		ActorKind: "human", Class: "move", Actor: "christopher", Topics: []string{"cli/view", "portability/mcp"},
		Index:  &types.ShownFactIndex{Title: "Composing views", Topic: "cli/view"},
		Refs:   []types.ShownRef{{ID: "20261101-173830-s-tac-hnv", Kind: "builds-on", Desc: "the partial types"}},
		Closes: []string{"20261101-170000-s-tac-gap"}, Supersedes: []string{"20261101-160000-d-tac-old"},
		Attachments: []string{"notes.md"}, Preflight: "bypassed",
		DerivedStatus: types.DerivedStatus{
			Kind: "superseded-by", By: "20261101-190000-d-tac-hed",
			SupersedePath: []string{primary.ID, "20261101-185000-d-tac-mid", "20261101-190000-d-tac-hed"},
		},
		Time:      captured,
		Procedure: &types.ShownProcedureSpec{Params: "mode: fast\nlimit: 3\n", Steps: "- id: first\n"},
	}}
	if !reflect.DeepEqual(shown, want) {
		t.Errorf("a group without primary should be skipped and the primary carried whole\n got: %+v\nwant: %+v", shown, want)
	}
}

func TestShowResult_ShownEntriesChainItems(t *testing.T) {
	gap := &model.Entry{ID: "20261101-100000-s-tac-gap", Type: model.TypeSignal, Kind: model.KindGap, Title: "Slow reindex", Summary: "Reindexing is slow. It blocks.", Content: "Body."}
	kindlessDecision := &model.Entry{ID: "20261101-110000-d-tac-old", Type: model.TypeDecision, Content: "Legacy decision. More."}
	kindlessSignal := &model.Entry{ID: "20261101-120000-s-tac-old", Type: model.TypeSignal, Content: "Legacy signal."}

	tests := []struct {
		name string
		item model.ShowTreeItem
		want types.ShownChainItem
	}{
		{
			name: "a resolved item carries its edge, kind and first sentence",
			item: model.ShowTreeItem{
				Entry: gap, Depth: 1, Relations: []string{"refs", "closes"}, RefKind: model.RefKindAddresses, RefDesc: "what it answers",
				Status: model.Status{Kind: model.StatusOpen},
			},
			want: types.ShownChainItem{
				ID: gap.ID, Title: "Slow reindex", Kind: "gap", Status: "open", Summary: "Reindexing is slow. It blocks.",
				Relation: "refs", RefKind: "addresses", Desc: "what it answers", Depth: 1,
				Relations: []string{"refs", "closes"}, KindLabel: "gap", FirstSentence: "Reindexing is slow.",
				DerivedStatus: types.DerivedStatus{Kind: "open"},
			},
		},
		{
			name: "a kindless decision is labelled directive and its sentence comes from the body",
			item: model.ShowTreeItem{Entry: kindlessDecision, Depth: 2, Relations: []string{"supersedes"}},
			want: types.ShownChainItem{
				ID: kindlessDecision.ID, Relation: "supersedes", Depth: 2, Relations: []string{"supersedes"},
				KindLabel: "directive", FirstSentence: "Legacy decision.",
			},
		},
		{
			name: "a kindless signal is labelled by its type",
			item: model.ShowTreeItem{Entry: kindlessSignal, Depth: 1, Relations: []string{"refd-by"}},
			want: types.ShownChainItem{
				ID: kindlessSignal.ID, Relation: "refd-by", Depth: 1, Relations: []string{"refd-by"},
				KindLabel: "signal", FirstSentence: "Legacy signal.",
			},
		},
		{
			name: "an unresolved cross-repo item names its repo and nothing of an entry",
			item: model.ShowTreeItem{CrossRepoID: "example.com/team/remote:20261101-130000-s-tac-rem", Depth: 1, Relations: []string{"refs"}},
			want: types.ShownChainItem{
				ID: "example.com/team/remote:20261101-130000-s-tac-rem", Relation: "refs", Depth: 1, Relations: []string{"refs"},
				UnresolvedRepo: "example.com/team/remote",
			},
		},
		{
			name: "a resolved cross-repo item shows its prefixed identity and no unresolved repo",
			item: model.ShowTreeItem{Entry: kindlessSignal, CrossRepoID: "example.com/team/remote:" + kindlessSignal.ID, Depth: 1},
			want: types.ShownChainItem{
				ID: "example.com/team/remote:" + kindlessSignal.ID, Depth: 1, KindLabel: "signal", FirstSentence: "Legacy signal.",
			},
		},
		{
			name: "an item shown elsewhere keeps where it was shown",
			item: model.ShowTreeItem{Entry: gap, Depth: 1, ShownAbove: true},
			want: types.ShownChainItem{
				ID: gap.ID, Title: "Slow reindex", Kind: "gap", Summary: "Reindexing is slow. It blocks.", Depth: 1,
				SeenInResponse: true, ShownAbove: true, KindLabel: "gap", FirstSentence: "Reindexing is slow.",
			},
		},
		{
			name: "a superseded item carries its trail and a truncated one its frontier",
			item: model.ShowTreeItem{
				Entry: kindlessDecision, Depth: 3, ShownBelow: true,
				Status:        model.Status{Kind: model.StatusSupersededBy, By: "20261101-140000-d-tac-hed"},
				SupersedePath: []string{kindlessDecision.ID, "20261101-140000-d-tac-hed"},
				Truncated:     []model.TruncatedRef{{ID: "20261101-090000-s-tac-one"}, {ID: "20261101-080000-s-tac-two"}}, TruncatedReason: "fan-out cap",
			},
			want: types.ShownChainItem{
				ID: kindlessDecision.ID, Status: "superseded-by 20261101-140000-d-tac-hed", Depth: 3, SeenInResponse: true, ShownBelow: true,
				Truncated: []string{"20261101-090000-s-tac-one", "20261101-080000-s-tac-two"}, TruncatedReason: "fan-out cap",
				KindLabel: "directive", FirstSentence: "Legacy decision.",
				DerivedStatus: types.DerivedStatus{
					Kind: "superseded-by", By: "20261101-140000-d-tac-hed",
					SupersedePath: []string{kindlessDecision.ID, "20261101-140000-d-tac-hed"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			primary := &model.Entry{ID: "20261101-150000-d-tac-pri", Type: model.TypeDecision}
			result := &query.ShowResult{Groups: []query.ShowGroup{{Primary: primary, Upstream: []model.ShowTreeItem{tt.item}, Downstream: []model.ShowTreeItem{tt.item}}}}
			shown, err := result.ShownEntries()
			if err != nil {
				t.Fatal(err)
			}
			if len(shown) != 1 {
				t.Fatalf("one primary should give one shown entry, got %+v", shown)
			}
			if !reflect.DeepEqual(shown[0].Upstream, []types.ShownChainItem{tt.want}) {
				t.Errorf("the upstream item should carry what the tree line reads\n got: %+v\nwant: %+v", shown[0].Upstream, tt.want)
			}
			if !reflect.DeepEqual(shown[0].Downstream, []types.ShownChainItem{tt.want}) {
				t.Errorf("the downstream item should map like the upstream one\n got: %+v\nwant: %+v", shown[0].Downstream, tt.want)
			}
		})
	}
}

func TestSearchResult_Hits(t *testing.T) {
	gap := &model.Entry{
		ID: "20261101-100000-s-tac-gap", Type: model.TypeSignal, Layer: model.LayerTactical, Kind: model.KindGap, Title: "Slow reindex",
		Confidence: "high", Participants: []string{"Christopher"}, Topics: []model.TopicPath{topicPath(t, "search")},
		Content: "Reindexing a large graph is slow.\nSecond line.",
	}
	done := &model.Entry{ID: "20261101-110000-s-tac-don", Type: model.TypeSignal, Layer: model.LayerTactical, Kind: model.KindDone, Closes: []string{gap.ID}, Content: "Fixed."}
	remoteGap := &model.Entry{ID: "20261101-120000-s-tac-rem", Type: model.TypeSignal, Layer: model.LayerTactical, Kind: model.KindGap, Summary: "Remote gap.", Intent: model.IntentGuiding}
	remoteDone := &model.Entry{ID: "20261101-130000-s-tac-rdn", Type: model.TypeSignal, Layer: model.LayerTactical, Kind: model.KindDone, Closes: []string{remoteGap.ID}}

	local := model.NewGraph([]*model.Entry{gap, done, remoteGap})
	remote := model.NewGraph([]*model.Entry{remoteGap, remoteDone})
	model.NewMultiGraph(local, []model.RepoID{"example.com/team/remote"}, func(repo model.RepoID) (*model.Graph, error) {
		if repo == "example.com/team/remote" {
			return remote, nil
		}
		return nil, nil
	})

	tests := []struct {
		name  string
		found query.SearchEntry
		want  []types.SearchHit
	}{
		{
			name: "a local hit derives status and topics in the local graph and carries its citations",
			found: query.SearchEntry{Entry: gap, Score: 0.9, Citations: []query.Citation{
				{Snippet: "slow", IsSummary: true, Score: 0.9},
				{Snippet: "notes", Breadcrumb: []string{"Approach", "Storage"}, SourceAttachmentPath: "notes.md", IsAttachment: true, Score: 0.5},
			}},
			want: []types.SearchHit{{
				ID: gap.ID, Title: "Slow reindex", Type: "signal", Kind: "gap", Layer: "tactical", Status: "closed-by " + done.ID, Score: 0.9,
				Confidence: "high", Participants: []string{"Christopher"}, Topics: []string{"search"},
				DerivedStatus: types.DerivedStatus{Kind: "closed-by", By: done.ID}, BodyExcerpt: "Reindexing a large graph is slow.",
				Citations: []types.SearchCitation{
					{Snippet: "slow", IsSummary: true, Score: 0.9},
					{Snippet: "notes", Breadcrumb: []string{"Approach", "Storage"}, AttachmentPath: "notes.md", IsAttachment: true, Score: 0.5},
				},
			}},
		},
		{
			name:  "a cross-graph hit derives its status in the member graph it came from",
			found: query.SearchEntry{Entry: remoteGap, Score: 0.4, RepoID: "example.com/team/remote"},
			want: []types.SearchHit{{
				ID: "example.com/team/remote:" + remoteGap.ID, Type: "signal", Kind: "gap", Layer: "tactical", Status: "closed-by " + remoteDone.ID,
				Summary: "Remote gap.", Score: 0.4, Intent: "guiding", DerivedStatus: types.DerivedStatus{Kind: "closed-by", By: remoteDone.ID},
			}},
		},
		{
			name:  "a cross-graph hit whose member is gone derives its status in the local graph",
			found: query.SearchEntry{Entry: remoteGap, Score: 0.3, RepoID: "example.com/team/gone"},
			want: []types.SearchHit{{
				ID: "example.com/team/gone:" + remoteGap.ID, Type: "signal", Kind: "gap", Layer: "tactical", Status: "open",
				Summary: "Remote gap.", Score: 0.3, Intent: "guiding", DerivedStatus: types.DerivedStatus{Kind: "open"},
			}},
		},
		{
			name:  "a hit without an entry is dropped",
			found: query.SearchEntry{Score: 0.2},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &query.SearchResult{Entries: []query.SearchEntry{tt.found}}
			if got := result.Hits(local); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("the hit should carry what the entry line and citations read\n got: %+v\nwant: %+v", got, tt.want)
			}
		})
	}
}
