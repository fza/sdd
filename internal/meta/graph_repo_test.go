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
		})
	}
}
