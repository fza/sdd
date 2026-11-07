package model_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

func TestGraphCloneCone(t *testing.T) {
	cases := []struct {
		graphDir string
		want     []string
	}{
		{graphDir: "", want: []string{".sdd"}},
		{graphDir: ".sdd/graph", want: []string{".sdd"}},
		{graphDir: "./.sdd/graph", want: []string{".sdd"}},
		{graphDir: "decisions", want: []string{".sdd", "decisions"}},
		{graphDir: "docs/graph", want: []string{".sdd", "docs/graph"}},
	}
	for _, tc := range cases {
		t.Run(tc.graphDir, func(t *testing.T) {
			got := model.GraphCloneCone(tc.graphDir)
			if !slices.Equal(got, tc.want) {
				t.Errorf("the cone should hold .sdd and a graph_dir outside it, got %v", got)
			}
		})
	}
}

func TestGraphRemoteURLs(t *testing.T) {
	ssh, https, err := model.GraphRemoteURLs("github.com/acme/app-sdd")
	if err != nil {
		t.Fatal(err)
	}
	if ssh != "git@github.com:acme/app-sdd.git" || https != "https://github.com/acme/app-sdd.git" {
		t.Errorf("both remotes should derive from the repository id, got %s and %s", ssh, https)
	}
	_, _, err = model.GraphRemoteURLs("no-host")
	if err == nil {
		t.Error("an invalid repository id should be refused")
	}
}

func TestInitialProtocol(t *testing.T) {
	cases := []struct{ origin, want string }{
		{origin: "https://github.com/acme/app.git", want: model.ProtocolHTTPS},
		{origin: "git@github.com:acme/app.git", want: model.ProtocolSSH},
		{origin: "https://gitlab.com/acme/app.git", want: model.ProtocolSSH},
		{origin: "", want: model.ProtocolSSH},
	}
	for _, tc := range cases {
		t.Run(tc.origin, func(t *testing.T) {
			got := model.InitialProtocol("github.com/acme/app-sdd", tc.origin)
			if got != tc.want {
				t.Errorf("the first protocol should follow a same-host origin and default to ssh, got %s", got)
			}
		})
	}
}

func TestProtocolOrder(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		recorded string
		since    time.Time
		first    string
	}{
		{name: "recorded https used first", recorded: model.ProtocolHTTPS, since: now.Add(-time.Hour), first: model.ProtocolHTTPS},
		{name: "after 24 hours on https ssh goes first", recorded: model.ProtocolHTTPS, since: now.Add(-model.HTTPSRetryAfter), first: model.ProtocolSSH},
		{name: "recorded ssh used first", recorded: model.ProtocolSSH, since: now.Add(-48 * time.Hour), first: model.ProtocolSSH},
		{name: "nothing recorded tries ssh first", first: model.ProtocolSSH},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			order := model.ProtocolOrder(tc.recorded, tc.since, now)
			if len(order) != 2 || order[0] != tc.first || order[0] == order[1] {
				t.Errorf("the order should try both protocols, the expected one first, got %v", order)
			}
		})
	}
}

func TestProtocolOrderOfASingleRemote(t *testing.T) {
	order := model.ProtocolOrder(model.SingleRemote, time.Time{}, time.Now())
	if len(order) != 1 || order[0] != model.SingleRemote {
		t.Errorf("a single-remote clone should try its one remote alone, got %v", order)
	}
}

func TestConnectedCloneSource(t *testing.T) {
	const repoID = model.RepoID("example.test/acme/other")
	cases := []struct {
		name string
		url  string
		want model.CloneSource
	}{
		{name: "the derived https form", url: "https://example.test/acme/other.git", want: model.CloneSource{First: model.ProtocolHTTPS}},
		{name: "the derived ssh form", url: "git@example.test:acme/other.git", want: model.CloneSource{First: model.ProtocolSSH}},
		{name: "https without .git", url: "https://example.test/acme/other", want: model.CloneSource{URL: "https://example.test/acme/other"}},
		{name: "an ssh host alias", url: "work:acme/other.git", want: model.CloneSource{URL: "work:acme/other.git"}},
		{name: "a local path", url: "/srv/git/other.git", want: model.CloneSource{URL: "/srv/git/other.git"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.ConnectedCloneSource(repoID, tc.url); got != tc.want {
				t.Errorf("only the exact derived forms should pick a protocol, got %+v", got)
			}
		})
	}
}

func TestRouteMergeAttributes(t *testing.T) {
	existing := "*.md merge=binary\nold/[0-9]*.md merge=sdd-entry\n.sdd/config.yaml merge=sdd-config\n*.txt text\n"
	routing := []string{".sdd/graph/x merge=sdd-entry", model.ConfigMergeAttributeLine}
	got := model.RouteMergeAttributes(existing, routing)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if !slices.Equal(lines[len(lines)-2:], routing) {
		t.Errorf("sdd's routing should come last:\n%s", got)
	}
	if strings.Contains(got, "old/") {
		t.Errorf("routing for an earlier graph_dir should go:\n%s", got)
	}
	if !strings.Contains(got, "*.md merge=binary\n") || !strings.Contains(got, "*.txt text\n") {
		t.Errorf("other lines should stay:\n%s", got)
	}
	if model.RouteMergeAttributes(got, routing) != got {
		t.Error("routing again should change nothing")
	}
}

func TestEnsureExcludeLine(t *testing.T) {
	got := model.EnsureExcludeLine("# git exclude\n*.swp", model.GraphCloneExcludeLine)
	if got != "# git exclude\n*.swp\n"+model.GraphCloneExcludeLine+"\n" {
		t.Errorf("the line should be appended on its own line, got %q", got)
	}
	if model.EnsureExcludeLine(got, model.GraphCloneExcludeLine) != got {
		t.Error("a present line should change nothing")
	}
}

func TestPlanCloneReplay(t *testing.T) {
	cases := []struct {
		name     string
		ahead    []model.CloneCommit
		recorded []string
		want     model.CloneReplay
	}{
		{name: "nothing ahead", want: model.CloneReplay{}},
		{name: "only recorded commits stay", ahead: []model.CloneCommit{{Hash: "a"}, {Hash: "m", Merge: true}}, recorded: []string{"a", "m"}, want: model.CloneReplay{}},
		{
			name:     "an unrecorded commit resets and replays sdd's own non-merge commits in order",
			ahead:    []model.CloneCommit{{Hash: "a"}, {Hash: "hand"}, {Hash: "m", Merge: true}, {Hash: "b"}},
			recorded: []string{"b", "a", "m"},
			want:     model.CloneReplay{Reset: true, Replay: []string{"a", "b"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := model.PlanCloneReplay(tc.ahead, tc.recorded)
			if got.Reset != tc.want.Reset || !slices.Equal(got.Replay, tc.want.Replay) {
				t.Errorf("the plan should drop exactly the unrecorded commits, got %+v", got)
			}
		})
	}
}
