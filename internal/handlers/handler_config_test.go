package handlers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

func newConfigTestHandler(t *testing.T) (*Handler, string, string) {
	t.Helper()
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "xdg", "sdd", "config.yaml")
	sddDir := filepath.Join(dir, "repo", ".sdd")
	if err := os.MkdirAll(sddDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reg := repos.NewRegistry(repos.Locations{
		ConfigPath: globalPath,
		CacheRoot:  filepath.Join(dir, "cache"),
	})
	h := New(Options{
		SDDDir: sddDir,
		Repos:  repos.NewManager(reg, git.CLI{}),
	})
	return h, globalPath, sddDir
}

func TestConfigSet_GlobalCreatesAndPreservesComments(t *testing.T) {
	h, globalPath, _ := newConfigTestHandler(t)

	if err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: "global", Key: "participant", Value: "Christopher"}); err != nil {
		t.Fatalf("first set: %v", err)
	}
	data, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("global file not created: %v", err)
	}
	if !strings.Contains(string(data), "participant: Christopher") {
		t.Errorf("participant not written: %s", data)
	}

	// A user comment survives subsequent sets.
	commented := "# my note\n" + string(data)
	if err := os.WriteFile(globalPath, []byte(commented), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: "global", Key: "llm.model", Value: "claude-sonnet-4-6"}); err != nil {
		t.Fatalf("second set: %v", err)
	}
	data, _ = os.ReadFile(globalPath)
	if !strings.Contains(string(data), "# my note") {
		t.Errorf("comment lost: %s", data)
	}
	if !strings.Contains(string(data), "model: claude-sonnet-4-6") {
		t.Errorf("nested key not written: %s", data)
	}
}

func TestConfigSet_LocalWritesRepoFile(t *testing.T) {
	h, _, sddDir := newConfigTestHandler(t)
	if err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: "local", Key: "embedding.provider", Value: "ollama"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(sddDir, "config.local.yaml"))
	if err != nil {
		t.Fatalf("local file not created: %v", err)
	}
	if !strings.Contains(string(data), "provider: ollama") {
		t.Errorf("embedding.provider not written: %s", data)
	}
}

// A key foreign to the target layer is rejected before anything lands on
// disk — the schema split doing its job at write time.
func TestConfigSet_RejectsForeignKeys(t *testing.T) {
	h, globalPath, sddDir := newConfigTestHandler(t)

	err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: "global", Key: "repo_id", Value: "a/b"})
	if err == nil || !strings.Contains(err.Error(), "repo_id") {
		t.Errorf("repo_id in global must be rejected, got %v", err)
	}
	if _, statErr := os.Stat(globalPath); !os.IsNotExist(statErr) {
		t.Error("rejected write must not create the global file")
	}

	err = h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: "local", Key: "bogus.key", Value: "x"})
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("unknown key in local must be rejected, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(sddDir, "config.local.yaml")); !os.IsNotExist(statErr) {
		t.Error("rejected write must not create the local file")
	}
}

// The value may be a secret typed into argv; the rejection names the key only.
func TestConfigSet_TypeErrorOmitsTheValue(t *testing.T) {
	h, _, sddDir := newConfigTestHandler(t)

	err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: "local", Key: "llm.api_keys", Value: "sk-test-123456"})
	if err == nil {
		t.Fatal("a scalar where a map belongs should be rejected")
	}
	if strings.Contains(err.Error(), "sk-test") {
		t.Errorf("the error should not show the value: %v", err)
	}
	if !strings.Contains(err.Error(), "llm.api_keys") {
		t.Errorf("the error should name the key: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(sddDir, "config.local.yaml")); !os.IsNotExist(statErr) {
		t.Error("rejected write must not create the local file")
	}
}

// A file holding a key a newer sdd wrote must stay writable, and keep that
// key — a whole-document rewrite would drop it, a whole-document probe would
// refuse the write (20260810-144515-s-tac-8ae).
func TestConfigSet_PreservesKeysFromAnotherVersion(t *testing.T) {
	h, globalPath, _ := newConfigTestHandler(t)
	if err := os.MkdirAll(filepath.Dir(globalPath), 0o700); err != nil {
		t.Fatal(err)
	}
	existing := "# keep me\nparticipant: Christopher\ntelemetry:\n  endpoint: https://example.invalid\n"
	if err := os.WriteFile(globalPath, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: "global", Key: "llm.model", Value: "sonnet"}); err != nil {
		t.Fatalf("a foreign key must not make the file unwritable: %v", err)
	}

	data, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# keep me", "telemetry:", "endpoint: https://example.invalid", "model: sonnet"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("write lost %q:\n%s", want, data)
		}
	}
}

// Registering a connection patches the `repos` list rather than rewriting the
// document.
func TestConnectRepo_PatchesRatherThanRewrites(t *testing.T) {
	h, globalPath, _ := newConfigTestHandler(t)
	if err := os.MkdirAll(filepath.Dir(globalPath), 0o700); err != nil {
		t.Fatal(err)
	}
	existing := "# my settings\nparticipant: Christopher\ntelemetry:\n  endpoint: https://example.invalid\n"
	if err := os.WriteFile(globalPath, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	repo := repos.ConnectedRepo{RepoID: "github.com/networkteam/other", CloneURL: "git@github.com:networkteam/other.git"}
	if err := h.connectRepo(repo); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# my settings", "telemetry:", "endpoint: https://example.invalid", "github.com/networkteam/other"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("connect lost %q:\n%s", want, data)
		}
	}

	cfg, err := repos.LoadConfigFrom(globalPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Connected(repo.RepoID); !ok {
		t.Errorf("connection did not round-trip: %+v", cfg.Repos)
	}
}

// Numeric values land as YAML numbers, not strings — a string in an int
// field would fail the schema probe.
func TestConfigSet_TypedScalars(t *testing.T) {
	h, globalPath, _ := newConfigTestHandler(t)
	if err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: "global", Key: "llm.concurrency", Value: "8"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(globalPath)
	if !strings.Contains(string(data), "concurrency: 8") {
		t.Errorf("concurrency not written as number: %s", data)
	}
}

func TestConfigSet_ClaudeCLICredentials(t *testing.T) {
	h, globalPath, _ := newConfigTestHandler(t)

	err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: "global", Key: "llm.claude_cli.credentials", Value: "apikey"})
	if err == nil || !strings.Contains(err.Error(), "login") || !strings.Contains(err.Error(), "env") {
		t.Errorf("an unknown credentials value should be refused, naming the allowed ones, got %v", err)
	}
	if _, statErr := os.Stat(globalPath); !os.IsNotExist(statErr) {
		t.Error("a refused write must not create the global file")
	}

	if err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: "global", Key: "llm.claude_cli.credentials", Value: "env"}); err != nil {
		t.Fatalf("env should be accepted: %v", err)
	}
	data, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := model.ParseConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.ClaudeCLI.Credentials != model.ClaudeCLICredentialsEnv {
		t.Errorf("credentials should land nested under llm.claude_cli: %s", data)
	}
}

// A read ignores a local value loosening the shared verify policy; a write
// refuses it before anything lands on disk.
func TestConfigSet_RefusesALocalVerifyValueLooseningTheShared(t *testing.T) {
	h, _, sddDir := newConfigTestHandler(t)
	if err := os.WriteFile(filepath.Join(sddDir, "config.yaml"), []byte("verify:\n  mode: deep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: "local", Key: "verify.mode", Value: "prose"})
	if err == nil || !strings.Contains(err.Error(), "may only raise") {
		t.Fatalf("a loosening local verify.mode should be refused naming why, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(sddDir, "config.local.yaml")); !os.IsNotExist(err) {
		t.Fatalf("a refused write should leave no local file, got %v", err)
	}
	if err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: "local", Key: "verify.mode", Value: "deep"}); err != nil {
		t.Fatalf("a local value matching the shared one should be written: %v", err)
	}
}
