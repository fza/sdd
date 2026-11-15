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

// probeEnv names the call a re-executed test binary makes under the guard.
const probeEnv = "SDD_TEST_NOLIVELLM_PROBE"

func TestMain(m *testing.M) {
	if os.Getenv(probeEnv) != "" {
		testsupport.RunWithoutLiveLLMs(m)
	}
	os.Exit(m.Run())
}

func TestRunWithoutLiveLLMs(t *testing.T) {
	cases := []struct {
		probe   string
		wantErr string
	}{
		{probe: "none"},
		{probe: "claude", wantErr: "tests started an installed CLI"},
		{probe: "codex", wantErr: "tests started an installed CLI"},
		{probe: "sdd", wantErr: "tests started an installed CLI"},
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
