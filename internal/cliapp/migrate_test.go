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

func TestWritesOnAnOlderGraphKeepItsSchema(t *testing.T) {
	project := newMigrateProject(t)

	output, err := runReleased(t, project, "", "new", "s", "tac", "--confidence", "high", "--skip-preflight", "--summary", "S.", "A new gap.")
	if err != nil {
		t.Fatalf("a write on an older graph should go through: %v\n%s", err, output)
	}
	written := gitIn(t, project, "show", "--name-only", "--format=", "HEAD")
	entry := readFile(t, filepath.Join(project, strings.TrimSpace(written)))
	if strings.Contains(entry, "\ntitle:") {
		t.Errorf("an entry on a schema-1 graph should carry no title:\n%s", entry)
	}
	if meta := readFile(t, filepath.Join(project, ".sdd", "meta.json")); !strings.Contains(meta, `"graph_schema_version": 1`) {
		t.Errorf("a write should leave the graph's schema alone:\n%s", meta)
	}

	output, err = runReleased(t, project, "", "new", "s", "tac", "--confidence", "high", "--skip-preflight", "--summary", "S.", "--title", "t", "Another gap.")
	if err == nil {
		t.Fatal("an explicit title on a schema-1 graph should be refused")
	}
	if !strings.Contains(err.Error()+output, "schema 1") {
		t.Errorf("the refusal should name the graph's schema:\n%v\n%s", err, output)
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

			meta := readFile(t, filepath.Join(project, ".sdd", "meta.json"))
			entry := readFile(t, filepath.Join(project, ".sdd", "graph", filepath.FromSlash(migrateEntryRel)))
			if !tc.wantMigrated {
				if !strings.Contains(meta, `"graph_schema_version": 1`) || strings.Contains(entry, "title:") {
					t.Errorf("an unapproved migration should change nothing:\n%s\n%s", meta, entry)
				}
				return
			}
			if !strings.Contains(meta, `"graph_schema_version": 2`) {
				t.Errorf("an approved migration should stamp the new schema version:\n%s", meta)
			}
			if !strings.Contains(entry, "title: Migrated title") || !strings.Contains(entry, "summary: A stored summary that stays.") {
				t.Errorf("the backfill should add the title and keep the stored summary:\n%s", entry)
			}
			if log := gitIn(t, project, "log", "--format=%s"); !strings.Contains(log, "sdd: migrate graph schema 1 -> 2") {
				t.Errorf("the stamp should land as its own commit:\n%s", log)
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

func TestMigrateStampOnlyDefersTheBackfill(t *testing.T) {
	project := newMigrateProject(t)
	entryPath := filepath.Join(project, ".sdd", "graph", filepath.FromSlash(migrateEntryRel))

	output, err := runReleased(t, project, "", "migrate", "--yes", "--stamp-only")
	if err != nil {
		t.Fatalf("sdd migrate --stamp-only: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Backfill skipped") {
		t.Errorf("the plan should say the backfill is deferred:\n%s", output)
	}
	if meta := readFile(t, filepath.Join(project, ".sdd", "meta.json")); !strings.Contains(meta, `"graph_schema_version": 2`) {
		t.Errorf("a stamp-only run should stamp the new schema version:\n%s", meta)
	}
	if entry := readFile(t, entryPath); strings.Contains(entry, "title:") {
		t.Errorf("a stamp-only run should leave entries as they are:\n%s", entry)
	}

	if output, err := runReleased(t, project, "", "migrate", "--yes"); err != nil {
		t.Fatalf("sdd migrate: %v\n%s", err, output)
	}
	if entry := readFile(t, entryPath); !strings.Contains(entry, "title: Migrated title") {
		t.Errorf("a later migrate on the stamped graph should fill the deferred titles:\n%s", entry)
	}
}

func TestMigrateCommitsTitlesAFailedCommitLeftBehind(t *testing.T) {
	project := newMigrateProject(t)
	if output, err := runReleased(t, project, "", "migrate", "--yes", "--stamp-only"); err != nil {
		t.Fatalf("stamp: %v\n%s", err, output)
	}
	entryPath := filepath.Join(project, ".sdd", "graph", filepath.FromSlash(migrateEntryRel))
	writeFile(t, entryPath, strings.Replace(readFile(t, entryPath), "summary:", "title: stranded title\nsummary:", 1))

	output, err := runReleased(t, project, "", "migrate", "--yes")
	if err != nil {
		t.Fatalf("sdd migrate: %v\n%s", err, output)
	}
	if !strings.Contains(output, "committed 1 entries an earlier backfill titled but did not commit") || !strings.Contains(output, "nothing to migrate") {
		t.Errorf("the stranded title should be committed and leave nothing to migrate:\n%s", output)
	}
	if status := gitIn(t, project, "status", "--porcelain", "--", ".sdd/graph"); strings.TrimSpace(status) != "" {
		t.Errorf("no graph file should stay uncommitted:\n%s", status)
	}
	if entry := readFile(t, entryPath); !strings.Contains(entry, "title: stranded title") {
		t.Errorf("the stranded title should be kept as written:\n%s", entry)
	}
}

func TestSummarizeAllOnAnOlderGraphOwesNoTitles(t *testing.T) {
	project := newMigrateProject(t)
	output, err := runReleased(t, project, "", "summarize", "--all")
	if err != nil {
		t.Fatalf("summarize --all: %v\n%s", err, output)
	}
	entry := readFile(t, filepath.Join(project, ".sdd", "graph", filepath.FromSlash(migrateEntryRel)))
	if strings.Contains(entry, "\ntitle:") {
		t.Errorf("a summarized entry on a schema-1 graph owes no title:\n%s", entry)
	}
}
