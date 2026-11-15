package test_test

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/test"
)

// probeEnv names the call a re-executed test binary makes under the guard.
const probeEnv = "SDD_TEST_NOLIVELLM_PROBE"

func TestMain(m *testing.M) {
	if os.Getenv(probeEnv) != "" {
		test.RunWithoutLiveLLMs(m)
	}
	os.Exit(m.Run())
}

func TestRunWithoutLiveLLMs(t *testing.T) {
	cases := []struct {
		probe   string
		wantErr string
	}{
		{probe: "none"},
		{probe: "claude", wantErr: "tests started an LLM CLI"},
		{probe: "codex", wantErr: "tests started an LLM CLI"},
		{probe: "ollama", wantErr: "tests called a live Ollama"},
	}
	for _, tc := range cases {
		t.Run(tc.probe, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestProbe$")
			cmd.Env = append(os.Environ(), probeEnv+"="+tc.probe)
			out, err := cmd.CombinedOutput()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("a run making no live call should pass: %v\n%s", err, out)
				}
				return
			}
			if err == nil || !strings.Contains(string(out), tc.wantErr) {
				t.Fatalf("a run making a live call should fail and name it: %v\n%s", err, out)
			}
		})
	}
}

// TestProbe runs only in the re-executed binary. It makes harmless calls, so
// a broken guard reaching a live CLI or Ollama changes nothing there.
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
		_ = exec.CommandContext(t.Context(), os.Getenv(probeEnv), "--version").Run()
	}
}
