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
	switch os.Getenv(probeEnv) {
	case "":
		os.Exit(m.Run())
	case "ollama-allowed":
		testsupport.RunWithoutLiveLLMs(m, testsupport.AllowOllama())
	default:
		testsupport.RunWithoutLiveLLMs(m)
	}
}

func TestRunWithoutLiveLLMs(t *testing.T) {
	const reached = "tests reached an installed CLI or a live Ollama"
	cases := []struct {
		name     string
		probe    string
		guardDir string
		wantErr  []string
	}{
		{name: "no live call", probe: "none"},
		{name: "claude", probe: "claude", wantErr: []string{reached, "claude --version"}},
		{name: "codex", probe: "codex", wantErr: []string{reached, "codex --version"}},
		{name: "sdd", probe: "sdd", wantErr: []string{reached, "sdd --version"}},
		{name: "ollama", probe: "ollama", wantErr: []string{reached, "ollama GET http://localhost:11434/api/version"}},
		{name: "ollama allowed", probe: "ollama-allowed"},
		{name: "a started run's call", probe: "nested", wantErr: []string{reached, "ollama GET http://localhost:11434/api/version"}},
		{name: "a guard dir no guarded run set up", probe: "claude", guardDir: t.TempDir(), wantErr: []string{guardDirEnv}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := probe(t, tc.probe, tc.guardDir)
			if len(tc.wantErr) == 0 {
				if err != nil {
					t.Fatalf("a run making no refused call should pass: %v\n%s", err, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("the run should fail:\n%s", out)
			}
			for _, want := range tc.wantErr {
				if !strings.Contains(out, want) {
					t.Errorf("the failure should name %q:\n%s", want, out)
				}
			}
		})
	}
}

func TestRunWithoutLiveLLMsRefusesLookAlikeStandIns(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"claude", "codex", "sdd"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env := []string{
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"GIT_CONFIG_GLOBAL=" + filepath.Join(dir, "gitconfig"),
		"GIT_CONFIG_NOSYSTEM=1",
	}
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME"} {
		env = append(env, name+"="+filepath.Join(dir, strings.ToLower(name)))
	}
	out, err := probe(t, "none", dir, env...)
	if err == nil || !strings.Contains(out, "stand-in claude is not the guard's") {
		t.Fatalf("a guard dir holding other stand-ins should be refused: %v\n%s", err, out)
	}
}

// probe re-runs the test binary under the guard, as a run started by a
// guarded run owning guardDir, or as a run of its own when guardDir is empty.
func probe(t *testing.T, call, guardDir string, env ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestProbe$")
	cmd.Env = append(os.Environ(), probeEnv+"="+call, guardDirEnv+"="+guardDir)
	cmd.Env = append(cmd.Env, env...)
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
		out, err := exec.CommandContext(t.Context(), "git", "config", "--type=bool", "--get", "maintenance.auto").Output()
		if err != nil || strings.TrimSpace(string(out)) != "false" {
			t.Errorf("git should run with maintenance.auto off, got %q, %v", out, err)
		}
	case "ollama-allowed":
		if _, ok := http.DefaultTransport.(*http.Transport); !ok {
			t.Errorf("AllowOllama should leave the default transport unwrapped, got %T", http.DefaultTransport)
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
	case "nested":
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestProbe$")
		cmd.Env = append(os.Environ(), probeEnv+"=ollama")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("a started run should leave the check to the run that started it: %v\n%s", err, out)
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
