package cliapp_test

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// In a project_dir layout a run starting in the work repository takes the
// work repository's local file over the project's own, writes --local there
// and names each file's values apart.
func TestWorkLocalLiesOverTheProjectLocal(t *testing.T) {
	work, project := pointedRepos(t)
	writeFile(t, filepath.Join(project, ".sdd", "config.local.yaml"), "participant: Ada\nllm:\n  model: project-model\n  timeout: 5m\nproject_only_typo: 1\n")

	out, err := runAppErr(t, work, "config", "set", "--local", "llm.model", "work-model")
	if err != nil {
		t.Fatalf("config set --local in the work repository: %v\n%s", err, out)
	}
	if got := readFile(t, filepath.Join(work, ".sdd", "config.local.yaml")); !strings.Contains(got, "work-model") {
		t.Errorf("--local should write the work repository's local file, got:\n%s", got)
	}
	if got := readFile(t, filepath.Join(project, ".sdd", "config.local.yaml")); strings.Contains(got, "work-model") {
		t.Errorf("the project's local file should stay as it was, got:\n%s", got)
	}

	out, err = runAppErr(t, work, "config", "get", "llm")
	if err != nil {
		t.Fatalf("config get llm: %v\n%s", err, out)
	}
	for _, want := range []string{`llm\.model\s+work-model\s+\(work local\)`, `llm\.timeout\s+5m\s+\(local\)`} {
		if !regexp.MustCompile(want).MatchString(out) {
			t.Errorf("config get should name each local file's values apart, want %s, got:\n%s", want, out)
		}
	}

	out, err = runAppErr(t, work, "config")
	if err != nil {
		t.Fatalf("config: %v\n%s", err, out)
	}
	if !regexp.MustCompile(`project_only_typo\s+\(`).MatchString(out) {
		t.Errorf("the ignored-keys list should name an unknown key in the project's local file, got:\n%s", out)
	}
}

// A server started in the work repository follows a change to the project's
// local file beneath the work repository's.
func TestPointedServeFollowsTheProjectLocal(t *testing.T) {
	work, project := pointedRepos(t)
	t.Chdir(work)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(work, "xdg-config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(work, "xdg-cache"))
	t.Setenv("XDG_DATA_HOME", dataHome(work))
	t.Setenv("XDG_STATE_HOME", filepath.Join(work, "xdg-state"))
	writeFile(t, filepath.Join(work, ".sdd", "config.local.yaml"), "llm:\n  model: work-model\n")
	fixture := &appFixture{root: work}

	fixture.serve(t, func(client *mcp.ClientSession) {
		session, _ := openSession(t, client)

		writeFile(t, filepath.Join(project, ".sdd", "config.local.yaml"), "participant: Ada\ndialogue:\n  mode: autonomous\n")
		resumed, failure := callToolResult(t, client, "resume_session", map[string]any{"session": session})
		if failure != "" {
			t.Fatalf("resume_session: %s", failure)
		}
		if notice := hostNotice(t, resumed); !strings.Contains(notice, "dialogue.mode: guided -> autonomous") {
			t.Errorf("a change to the project's local file should take effect while serving, got %q", notice)
		}
	})
}
