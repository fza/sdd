package meta_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/meta"
)

func writeConfig(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, ".sdd", name)
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(path, []byte(content), 0o644)
	if err != nil {
		t.Fatal(err)
	}
}

func TestResolveProject(t *testing.T) {
	cases := []struct {
		name      string
		work      map[string]string
		project   map[string]string
		wantRoot  string
		wantError string
	}{
		{
			name:     "a project of its own resolves to itself",
			work:     map[string]string{"config.yaml": "graph_dir: .sdd/graph\nrepo_id: example.test/work\n"},
			wantRoot: "work",
		},
		{
			name:     "project_dir resolves against the work root",
			work:     map[string]string{"config.yaml": "project_dir: ../project\nskill_scope: project\nsupported_agents: [claude]\n"},
			project:  map[string]string{"config.yaml": "graph_dir: .sdd/graph\n"},
			wantRoot: "project",
		},
		{
			name:     "the local file moves the project",
			work:     map[string]string{"config.yaml": "project_dir: ../missing\n", "config.local.yaml": "project_dir: ../project\n"},
			project:  map[string]string{"config.yaml": "graph_dir: .sdd/graph\n"},
			wantRoot: "project",
		},
		{
			name:      "a project setting beside the pointer",
			work:      map[string]string{"config.yaml": "project_dir: ../project\nlanguage: de\n"},
			project:   map[string]string{"config.yaml": "graph_dir: .sdd/graph\n"},
			wantError: "sets language",
		},
		{
			name:      "an agent key in the local file",
			work:      map[string]string{"config.yaml": "project_dir: ../project\n", "config.local.yaml": "supported_agents: [codex]\n"},
			project:   map[string]string{"config.yaml": "graph_dir: .sdd/graph\n"},
			wantError: "sets supported_agents",
		},
		{
			name:      "a project that only has a local file",
			work:      map[string]string{"config.yaml": "project_dir: ../project\n"},
			project:   map[string]string{"config.local.yaml": "participant: Ada\n"},
			wantError: "clone the project there",
		},
		{
			name:      "a project pointing on",
			work:      map[string]string{"config.yaml": "project_dir: ../project\n"},
			project:   map[string]string{"config.yaml": "project_dir: ../work\n"},
			wantError: "itself sets project_dir",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			work := filepath.Join(root, "work")
			for name, content := range tc.work {
				writeConfig(t, work, name, content)
			}
			for name, content := range tc.project {
				writeConfig(t, filepath.Join(root, "project"), name, content)
			}

			project, err := meta.ResolveProject(work)

			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("the pointer should be refused with the reason: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if project.Root != filepath.Join(root, tc.wantRoot) {
				t.Errorf("the project root should be the %s folder, got %s", tc.wantRoot, project.Root)
			}
			if project.WorkRoot != work {
				t.Errorf("the work root should stay where .sdd/ was found, got %s", project.WorkRoot)
			}
			if project.Pointed() != (tc.wantRoot != "work") {
				t.Error("Pointed should report whether the project lives elsewhere")
			}
		})
	}
}

func TestResolveProjectReportsAMissingProject(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "work")
	writeConfig(t, work, "config.yaml", "project_dir: ../nowhere\n")

	_, err := meta.ResolveProject(work)

	if !errors.Is(err, meta.ErrNotAProject) {
		t.Errorf("a missing project should be reported as not an sdd project: %v", err)
	}
}
