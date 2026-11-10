package meta_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

func TestResolveProjectFollowsGraphRepo(t *testing.T) {
	cases := []struct {
		name      string
		work      map[string]string
		wantError string
	}{
		{
			name: "graph_repo with the stock guard resolves to the clone",
			work: map[string]string{"config.yaml": "graph_repo: example.test/acme/app-sdd\ngraph_dir: .sdd/GRAPH-ELSEWHERE.md\nskill_scope: project\n"},
		},
		{
			name: "with both pointers set the clone wins",
			work: map[string]string{"config.yaml": "graph_repo: example.test/acme/app-sdd\nproject_dir: ../project\n"},
		},
		{
			name: "the local file may name the graph repository",
			work: map[string]string{"config.yaml": "skill_scope: project\n", "config.local.yaml": "graph_repo: example.test/acme/app-sdd\n"},
		},
		{
			name: "the local file holds the person's own settings",
			work: map[string]string{"config.yaml": "graph_repo: example.test/acme/app-sdd\n", "config.local.yaml": "participant: Ada\nconnected:\n  write: true\nsync:\n  mode: auto\n"},
		},
		{
			name:      "a project setting beside graph_repo",
			work:      map[string]string{"config.yaml": "graph_repo: example.test/acme/app-sdd\nlanguage: de\n"},
			wantError: "sets language",
		},
		{
			name:      "a malformed repository id",
			work:      map[string]string{"config.yaml": "graph_repo: app-sdd\n"},
			wantError: "graph_repo",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			work := filepath.Join(root, "work")
			for name, content := range tc.work {
				writeConfig(t, work, name, content)
			}
			clones := filepath.Join(root, "clones")
			var asked model.RepoID
			project, err := meta.ResolveProject(work, func(id model.RepoID) (string, error) {
				asked = id
				return filepath.Join(clones, string(id)), nil
			})
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("the pointer should be refused with the reason: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if asked != "example.test/acme/app-sdd" || project.GraphRepo != asked {
				t.Errorf("the graph repository should be the one configured, got %q", asked)
			}
			if project.Root != filepath.Join(clones, "example.test/acme/app-sdd") || project.WorkRoot != work {
				t.Errorf("the project should be the clone, worked on from the code repository, got %+v", project)
			}
			if project.LocalConfigPath("") != filepath.Join(work, ".sdd", "config.local.yaml") {
				t.Errorf("the local layer should be the code repository's, got %q", project.LocalConfigPath(""))
			}
		})
	}
}

// A repository init switches to a graph repository resolves as that graph
// repository's project while it still points through project_dir, so its
// local layer may hold the settings a graph_repo project keeps there.
func TestResolveSwitchedProject(t *testing.T) {
	const switched = model.RepoID("example.test/acme/next-sdd")
	cases := []struct {
		name      string
		work      map[string]string
		wantRepo  model.RepoID
		wantError string
	}{
		{
			name:     "project_dir with project settings in the local file",
			work:     map[string]string{"config.yaml": "project_dir: ../project\nskill_scope: project\nsupported_agents: [claude]\n", "config.local.yaml": "participant: Ada\ndialogue:\n  mode: guided\n"},
			wantRepo: switched,
		},
		{
			name:     "graph_repo naming another graph repository",
			work:     map[string]string{"config.yaml": "graph_repo: example.test/acme/app-sdd\n"},
			wantRepo: switched,
		},
		{
			name:     "a project of its own resolves as itself",
			work:     map[string]string{"config.yaml": "graph_dir: .sdd/graph\nlanguage: en\n"},
			wantRepo: "",
		},
		{
			name:      "a project setting beside project_dir",
			work:      map[string]string{"config.yaml": "project_dir: ../project\nlanguage: de\n"},
			wantError: "sets language",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			work := filepath.Join(root, "work")
			for name, content := range tc.work {
				writeConfig(t, work, name, content)
			}
			clones := filepath.Join(root, "clones")
			project, err := meta.ResolveSwitchedProject(work, switched, func(id model.RepoID) (string, error) {
				return filepath.Join(clones, string(id)), nil
			})
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("the repository should be refused with the reason: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if project.GraphRepo != tc.wantRepo {
				t.Errorf("the project should be the one the switch names, got %q", project.GraphRepo)
			}
			if tc.wantRepo != "" && project.LocalConfigPath("") != filepath.Join(work, ".sdd", "config.local.yaml") {
				t.Errorf("the local layer should be the code repository's, got %q", project.LocalConfigPath(""))
			}
		})
	}
}

func TestProjectLocalConfigPath(t *testing.T) {
	cases := []struct {
		name     string
		project  meta.Project
		override string
		want     string
	}{
		{name: "graph_repo uses the code repository's file", project: meta.Project{Root: "/clone", WorkRoot: "/code", GraphRepo: "example.test/acme/app-sdd"}, want: "/code/.sdd/config.local.yaml"},
		{name: "--local-config wins", project: meta.Project{Root: "/clone", WorkRoot: "/code", GraphRepo: "example.test/acme/app-sdd"}, override: "/elsewhere.yaml", want: "/elsewhere.yaml"},
		{name: "a project of its own keeps the file under its .sdd", project: meta.Project{Root: "/code", WorkRoot: "/code"}},
		{name: "project_dir keeps the project's file", project: meta.Project{Root: "/project", WorkRoot: "/code"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.project.LocalConfigPath(tc.override); got != tc.want {
				t.Errorf("the local layer should come from the checkout a person works in, got %q", got)
			}
		})
	}
}
