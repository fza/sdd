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
	releasedVersion  = "1.2.3"
	untitledEntryRel = "2026/09/14-020000-s-tac-mig.md"
)

// newUntitledProject builds a schema-1 graph with one summarized, untitled
// entry, whose LLM is a local listener answering every call with a title and
// a summary.
func newUntitledProject(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			_, _ = w.Write([]byte(`{"models":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"model":"m","response":"{\"title\":\"filled title\",\"summary\":\"A generated summary.\"}","done":true}`))
	}))
	t.Cleanup(srv.Close)

	project := canonicalTemp(t)
	initRepo(t, project)
	writeFile(t, filepath.Join(project, ".sdd", "meta.json"), "{\n  \"graph_schema_version\": 1\n}\n")
	writeFile(t, filepath.Join(project, ".sdd", "config.yaml"), "graph_dir: .sdd/graph\nllm:\n  provider: ollama\n  model: m\n  ollama_endpoint: "+srv.URL+"\nverify:\n  mode: off\n")
	writeFile(t, filepath.Join(project, ".sdd", "config.local.yaml"), "participant: Ada\n")
	writeFile(t, filepath.Join(project, ".sdd", "graph", filepath.FromSlash(untitledEntryRel)), "---\ntype: signal\nkind: gap\nlayer: tactical\nconfidence: high\nsummary: A stored summary that stays.\n---\n\nThe body.\n")
	gitIn(t, project, "add", ".sdd")
	gitIn(t, project, "commit", "-q", "-m", "graph")
	return project
}

func runReleased(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg-config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "xdg-cache"))
	t.Setenv("XDG_DATA_HOME", dataHome(dir))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "xdg-state"))
	var stdout, stderr bytes.Buffer
	command := cliapp.New(cliapp.Options{Version: releasedVersion, Reader: strings.NewReader(""), Writer: &stdout, ErrWriter: &stderr})
	err := command.Run(t.Context(), append([]string{"sdd"}, args...))
	return stdout.String() + stderr.String(), err
}

func TestSummarizeCostLine(t *testing.T) {
	cases := []struct {
		name         string
		args         []string
		wantCostLine bool
		wantLine     string
		wantSummary  string
	}{
		{name: "a fill states its cost and keeps the stored summary", args: []string{"summarize", "--all"}, wantCostLine: true, wantLine: "  titled 20260914-020000-s-tac-mig", wantSummary: "summary: A stored summary that stays."},
		{name: "--force regenerates without a fill's cost line", args: []string{"summarize", "--all", "--force"}, wantLine: "  summarized 20260914-020000-s-tac-mig", wantSummary: "summary: A generated summary."},
		{name: "a named entry regenerates without a fill's cost line", args: []string{"summarize", "20260914-020000-s-tac-mig"}, wantLine: "  summarized 20260914-020000-s-tac-mig", wantSummary: "summary: A generated summary."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			project := newUntitledProject(t)
			output, err := runReleased(t, project, tc.args...)
			if err != nil {
				t.Fatalf("sdd summarize: %v\n%s", err, output)
			}
			costLine := "Backfill: 1 entries get a title beside their stored summary, 0 entries get a summary and a title -> 1 model calls."
			if strings.Contains(output, costLine) != tc.wantCostLine {
				t.Errorf("only a fill should state its cost before it runs:\n%s", output)
			}
			if strings.Contains(output, "Proceed?") {
				t.Errorf("summarize should run without asking:\n%s", output)
			}
			if !strings.Contains(output, tc.wantLine) {
				t.Errorf("off a terminal each filled entry should get its own line:\n%s", output)
			}
			entry := readFile(t, filepath.Join(project, ".sdd", "graph", filepath.FromSlash(untitledEntryRel)))
			if !strings.Contains(entry, "title: Filled title") || !strings.Contains(entry, tc.wantSummary) {
				t.Errorf("the entry should gain its title beside the expected summary:\n%s", entry)
			}
		})
	}
}

func TestSummarizeAllCommitsTitlesAFailedCommitLeftBehind(t *testing.T) {
	project := newUntitledProject(t)
	entryPath := filepath.Join(project, ".sdd", "graph", filepath.FromSlash(untitledEntryRel))
	writeFile(t, entryPath, strings.Replace(readFile(t, entryPath), "summary:", "title: stranded title\nsummary:", 1))

	output, err := runReleased(t, project, "summarize", "--all")
	if err != nil {
		t.Fatalf("summarize --all: %v\n%s", err, output)
	}
	if !strings.Contains(output, "committed 1 entries an earlier backfill titled but did not commit") || !strings.Contains(output, "-> 0 model calls") {
		t.Errorf("the stranded title should be committed before the cost is counted:\n%s", output)
	}
	if status := gitIn(t, project, "status", "--porcelain", "--", ".sdd/graph"); strings.TrimSpace(status) != "" {
		t.Errorf("no graph file should stay uncommitted:\n%s", status)
	}
	if entry := readFile(t, entryPath); !strings.Contains(entry, "title: stranded title") {
		t.Errorf("the stranded title should be kept as written:\n%s", entry)
	}
}
