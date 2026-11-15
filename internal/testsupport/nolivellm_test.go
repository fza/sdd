package testsupport_test

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/testsupport"
)

const (
	// probeEnv names the call a re-executed test binary makes under the guard.
	probeEnv = "SDD_TEST_NOLIVELLM_PROBE"
	// guardDirEnv hands a guarded run's directory to the binaries it starts.
	guardDirEnv = "SDD_TEST_GUARD_DIR"
)

func TestMain(m *testing.M) {
	if os.Getenv(probeEnv) != "" {
		testsupport.RunWithoutLiveLLMs(m)
	}
	os.Exit(m.Run())
}

func TestRunWithoutLiveLLMs(t *testing.T) {
	const reached = "tests reached an installed CLI or a live Ollama"
	cases := []struct {
		probe    string
		wantCall string
	}{
		{probe: "none"},
		{probe: "claude", wantCall: "claude --version"},
		{probe: "codex", wantCall: "codex --version"},
		{probe: "sdd", wantCall: "sdd --version"},
		{probe: "ollama", wantCall: "ollama GET http://localhost:11434/api/version"},
	}
	for _, tc := range cases {
		t.Run(tc.probe, func(t *testing.T) {
			out, err := probe(t, tc.probe, "")
			if tc.wantCall == "" {
				if err != nil {
					t.Fatalf("a run making no live call should pass: %v\n%s", err, out)
				}
				return
			}
			if err == nil || !strings.Contains(out, reached) || !strings.Contains(out, tc.wantCall) {
				t.Fatalf("a run making a live call should fail and name it: %v\n%s", err, out)
			}
		})
	}
}

func TestRunWithoutLiveLLMsRecordsIntoTheStartingRun(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := probe(t, "ollama", dir)
	if err != nil {
		t.Fatalf("a started run should leave the check to the run that started it: %v\n%s", err, out)
	}
	calls, err := os.ReadFile(filepath.Join(dir, "bin", "calls"))
	if err != nil || !strings.Contains(string(calls), "ollama GET http://localhost:11434/api/version") {
		t.Fatalf("a started run should record its calls into the starting run's directory: %v %q", err, calls)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Errorf("a started run should set up no guard of its own: %v %v", err, entries)
	}
}

// probe re-runs the test binary under the guard, as a run started by the
// guarded run owning guardDir, or as a run of its own when guardDir is empty.
func probe(t *testing.T, call, guardDir string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestProbe$")
	cmd.Env = append(os.Environ(), probeEnv+"="+call, guardDirEnv+"="+guardDir)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestProbe runs only in the re-executed binary. Its calls are harmless, and
// it runs a CLI only from the guard's stand-ins, so a broken guard never
// starts an installed one.
func TestProbe(t *testing.T) {
	switch os.Getenv(probeEnv) {
	case "":
		t.Skip("runs only under TestRunWithoutLiveLLMs")
	case "none":
		claude, err := exec.LookPath("claude")
		if err != nil {
			t.Fatal(err)
		}
		root := filepath.Dir(filepath.Dir(claude))
		for _, name := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME"} {
			if filepath.Dir(os.Getenv(name)) != root {
				t.Errorf("%s should point into the run's own temporary directory, got %q", name, os.Getenv(name))
			}
		}
		if filepath.Dir(os.Getenv("GIT_CONFIG_GLOBAL")) != root || os.Getenv("GIT_CONFIG_NOSYSTEM") != "1" {
			t.Errorf("git should read neither the user's nor the system's config, got GIT_CONFIG_GLOBAL=%q GIT_CONFIG_NOSYSTEM=%q",
				os.Getenv("GIT_CONFIG_GLOBAL"), os.Getenv("GIT_CONFIG_NOSYSTEM"))
		}
	case "ollama":
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost:11434/api/version", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
	default:
		name := os.Getenv(probeEnv)
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		standIns := filepath.Join(filepath.Dir(os.Getenv("GIT_CONFIG_GLOBAL")), "bin")
		if filepath.Dir(path) != standIns {
			t.Fatalf("refusing to run %s from outside the guard's stand-ins: %s", name, path)
		}
		_ = exec.CommandContext(t.Context(), path, "--version").Run()
	}
}
