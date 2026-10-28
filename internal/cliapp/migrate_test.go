package cliapp_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/cliapp"
)

const (
	migrateReleaseVersion = "1.2.3"
	migrateEntryRel       = "2026/09/14-020000-s-tac-mig.md"
)

// newMigrateProject builds a schema-1 graph with one summarized, untitled
// entry, whose LLM is a local listener answering every call with a title.
func newMigrateProject(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			_, _ = w.Write([]byte(`{"models":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"model":"m","response":"{\"title\":\"migrated title\"}","done":true}`))
	}))
	t.Cleanup(srv.Close)

	project := canonicalTemp(t)
	initRepo(t, project)
	writeFile(t, filepath.Join(project, ".sdd", "meta.json"), "{\n  \"graph_schema_version\": 1\n}\n")
	writeFile(t, filepath.Join(project, ".sdd", "config.yaml"), "graph_dir: .sdd/graph\nllm:\n  provider: ollama\n  model: m\n  ollama_endpoint: "+srv.URL+"\n")
	writeFile(t, filepath.Join(project, ".sdd", "config.local.yaml"), "participant: Ada\n")
	writeFile(t, filepath.Join(project, ".sdd", "graph", filepath.FromSlash(migrateEntryRel)), "---\ntype: signal\nkind: gap\nlayer: tactical\nconfidence: high\nsummary: A stored summary that stays.\n---\n\nThe body.\n")
	gitIn(t, project, "add", ".sdd")
	gitIn(t, project, "commit", "-q", "-m", "graph")
	return project
}

func runReleased(t *testing.T, dir, stdin string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg-config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "xdg-cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "xdg-state"))
	var stdout, stderr bytes.Buffer
	command := cliapp.New(cliapp.Options{Version: migrateReleaseVersion, Reader: strings.NewReader(stdin), Writer: &stdout, ErrWriter: &stderr})
	err := command.Run(t.Context(), append([]string{"sdd"}, args...))
	return stdout.String() + stderr.String(), err
}

func TestWritesOnASchemaOneGraphCarryTitles(t *testing.T) {
	project := newMigrateProject(t)

	output, err := runReleased(t, project, "", "new", "s", "tac", "--confidence", "high", "--skip-preflight", "--summary", "S.", "A new gap.")
	if err != nil {
		t.Fatalf("a write on a schema-1 graph should go through: %v\n%s", err, output)
	}
	written := gitIn(t, project, "show", "--name-only", "--format=", "HEAD")
	entry := readFile(t, filepath.Join(project, strings.TrimSpace(written)))
	if !strings.Contains(entry, "\ntitle: ") {
		t.Errorf("an entry on a schema-1 graph should carry a generated title:\n%s", entry)
	}
	if meta := readFile(t, filepath.Join(project, ".sdd", "meta.json")); !strings.Contains(meta, `"graph_schema_version": 1`) {
		t.Errorf("a write should leave the graph's schema alone:\n%s", meta)
	}

	output, err = runReleased(t, project, "", "new", "s", "tac", "--confidence", "high", "--skip-preflight", "--summary", "S.", "--title", "Explicit title", "Another gap.")
	if err != nil {
		t.Fatalf("an explicit title on a schema-1 graph should be accepted: %v\n%s", err, output)
	}
	written = gitIn(t, project, "show", "--name-only", "--format=", "HEAD")
	if entry := readFile(t, filepath.Join(project, strings.TrimSpace(written))); !strings.Contains(entry, "title: Explicit title") {
		t.Errorf("the explicit title should be written as given:\n%s", entry)
	}
}

func TestMigrate(t *testing.T) {
	cases := []struct {
		name         string
		stdin        string
		args         []string
		wantMigrated bool
	}{
		{name: "no answer leaves the graph unchanged", args: []string{"migrate"}},
		{name: "a refusal leaves the graph unchanged", stdin: "n\n", args: []string{"migrate"}},
		{name: "an approval migrates", stdin: "y\n", args: []string{"migrate"}, wantMigrated: true},
		{name: "--yes migrates without asking", args: []string{"migrate", "--yes"}, wantMigrated: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			project := newMigrateProject(t)
			output, err := runReleased(t, project, tc.stdin, tc.args...)
			if err != nil {
				t.Fatalf("sdd migrate: %v\n%s", err, output)
			}
			if !strings.Contains(output, "1 entries get a title beside their stored summary") {
				t.Errorf("the plan should state the backfill before it runs:\n%s", output)
			}
			if strings.Contains(output, "stamped") {
				t.Errorf("the plan for a graph at the current schema should promise no stamp:\n%s", output)
			}

			meta := readFile(t, filepath.Join(project, ".sdd", "meta.json"))
			entry := readFile(t, filepath.Join(project, ".sdd", "graph", filepath.FromSlash(migrateEntryRel)))
			if !tc.wantMigrated {
				if !strings.Contains(meta, `"graph_schema_version": 1`) || strings.Contains(entry, "title:") {
					t.Errorf("an unapproved migration should change nothing:\n%s\n%s", meta, entry)
				}
				return
			}
			if !strings.Contains(meta, `"graph_schema_version": 1`) {
				t.Errorf("filling titles should leave the schema version as it is:\n%s", meta)
			}
			if !strings.Contains(entry, "title: migrated title") || !strings.Contains(entry, "summary: A stored summary that stays.") {
				t.Errorf("the backfill should add the title and keep the stored summary:\n%s", entry)
			}
			if log := gitIn(t, project, "log", "--format=%s"); strings.Contains(log, "migrate graph schema") {
				t.Errorf("a graph at the current schema should see no stamp commit:\n%s", log)
			}
		})
	}
}

func TestMigrateOnACurrentGraphOwesNothing(t *testing.T) {
	project := newMigrateProject(t)
	if _, err := runReleased(t, project, "", "migrate", "--yes"); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	before := gitIn(t, project, "rev-parse", "HEAD")
	output, err := runReleased(t, project, "", "migrate")
	if err != nil {
		t.Fatalf("second migrate: %v\n%s", err, output)
	}
	if !strings.Contains(output, "nothing to migrate") {
		t.Errorf("a migrated graph should report nothing owed:\n%s", output)
	}
	if after := gitIn(t, project, "rev-parse", "HEAD"); after != before {
		t.Error("a migrated graph should see no further commit")
	}
}

func TestMigrateStampOnlyOnACurrentGraphChangesNothing(t *testing.T) {
	project := newMigrateProject(t)
	before := gitIn(t, project, "rev-parse", "HEAD")
	output, err := runReleased(t, project, "", "migrate", "--yes", "--stamp-only")
	if err != nil {
		t.Fatalf("sdd migrate --stamp-only: %v\n%s", err, output)
	}
	if !strings.Contains(output, "nothing to migrate") {
		t.Errorf("a stamp-only run on a current graph should report nothing owed:\n%s", output)
	}
	entry := readFile(t, filepath.Join(project, ".sdd", "graph", filepath.FromSlash(migrateEntryRel)))
	if strings.Contains(entry, "title:") {
		t.Errorf("a stamp-only run should leave entries as they are:\n%s", entry)
	}
	if after := gitIn(t, project, "rev-parse", "HEAD"); after != before {
		t.Error("a stamp-only run on a current graph should see no commit")
	}
}

func TestSummarizeAllFillsTitlesOnASchemaOneGraph(t *testing.T) {
	project := newMigrateProject(t)
	output, err := runReleased(t, project, "", "summarize", "--all")
	if err != nil {
		t.Fatalf("summarize --all: %v\n%s", err, output)
	}
	entry := readFile(t, filepath.Join(project, ".sdd", "graph", filepath.FromSlash(migrateEntryRel)))
	if !strings.Contains(entry, "title: migrated title") {
		t.Errorf("a summarized entry on a schema-1 graph should gain its title:\n%s", entry)
	}
}
