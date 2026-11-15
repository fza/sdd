package finders_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/internal/repos"
)

func TestUnknownConfigKeysReadsPastTheRetiredSyncMode(t *testing.T) {
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "global.yaml")
	sddDir := filepath.Join(dir, ".sdd")
	files := map[string]string{
		globalPath:                                 "sync:\n  mode: sometimes\n",
		filepath.Join(sddDir, "config.yaml"):       "repo_id: example.com/r\nsync:\n  mode: auto\n",
		filepath.Join(sddDir, "config.local.yaml"): "sync:\n  mode: always\n",
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f := finders.New(finders.Options{Repos: repos.NewRegistry(repos.Locations{ConfigPath: globalPath, CacheRoot: filepath.Join(dir, "cache")})})

	result, err := f.UnknownConfigKeys(query.UnknownConfigKeysQuery{SDDDir: sddDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Keys) != 0 || len(result.Ignored) != 0 {
		t.Errorf("the retired sync.mode should be a known key no layer warns about, got %+v", result)
	}
}

func TestUnknownConfigKeysReportsGitLimitsOutsideTheGlobalConfig(t *testing.T) {
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "global.yaml")
	sddDir := filepath.Join(dir, ".sdd")
	limits := "sync:\n  cooldown: 15m\n  default:\n    fetch_cooldown: 10m\n    ssh:\n      per_hour: 100\n  hosts:\n    gitlab.com:\n      https:\n        per_hour_unauthenticated: 30\n"
	committedPath := filepath.Join(sddDir, "config.yaml")
	localPath := filepath.Join(sddDir, "config.local.yaml")
	files := map[string]string{
		globalPath:    limits,
		committedPath: "repo_id: example.com/r\n" + limits,
		localPath:     limits,
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f := finders.New(finders.Options{Repos: repos.NewRegistry(repos.Locations{ConfigPath: globalPath, CacheRoot: filepath.Join(dir, "cache")})})

	result, err := f.UnknownConfigKeys(query.UnknownConfigKeysQuery{SDDDir: sddDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Keys) != 0 {
		t.Errorf("the git limits and the retired sync.cooldown are known keys, got %+v", result.Keys)
	}
	got := map[string][]string{}
	for _, s := range result.Ignored {
		if s.Reason == "" {
			t.Errorf("an ignored setting should say why, got %+v", s)
		}
		got[s.File] = append(got[s.File], s.Key)
	}
	if len(got[globalPath]) != 0 {
		t.Errorf("the global config may set the git limits, got %q", got[globalPath])
	}
	for _, path := range []string{committedPath, localPath} {
		if strings.Join(got[path], ",") != "sync.default,sync.hosts" {
			t.Errorf("git limits in %s should be reported as misplaced, got %q", path, got[path])
		}
	}
}

func TestUnknownConfigKeysReportsTheCommittedDialogueMode(t *testing.T) {
	dir := t.TempDir()
	sddDir := filepath.Join(dir, ".sdd")
	committedPath := filepath.Join(sddDir, "config.yaml")
	if err := os.MkdirAll(sddDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(committedPath, []byte("dialogue:\n  mode: autonomous\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := finders.New(finders.Options{Repos: repos.NewRegistry(repos.Locations{ConfigPath: filepath.Join(dir, "global.yaml"), CacheRoot: filepath.Join(dir, "cache")})})

	result, err := f.UnknownConfigKeys(query.UnknownConfigKeysQuery{SDDDir: sddDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Ignored) != 1 {
		t.Fatalf("the committed dialogue mode should be reported once as ignored, got %+v", result.Ignored)
	}
	if got := result.Ignored[0]; got.File != committedPath || got.Key != "dialogue.mode" || !strings.Contains(got.Reason, "config.local.yaml") {
		t.Errorf("the ignored setting should name its file, its key and where it belongs, got %+v", got)
	}
}

func TestUnknownConfigKeysReportsMisplacedVerifyValues(t *testing.T) {
	dir := t.TempDir()
	sddDir := filepath.Join(dir, ".sdd")
	if err := os.MkdirAll(sddDir, 0o755); err != nil {
		t.Fatal(err)
	}
	committedPath := filepath.Join(sddDir, "config.yaml")
	localPath := filepath.Join(sddDir, "config.local.yaml")
	if err := os.WriteFile(committedPath, []byte("verify:\n  mode: deep\n  driver:\n    command: run-it\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localPath, []byte("verify:\n  mode: prose\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := finders.New(finders.Options{Repos: repos.NewRegistry(repos.Locations{ConfigPath: filepath.Join(dir, "global.yaml"), CacheRoot: filepath.Join(dir, "cache")})})

	result, err := f.UnknownConfigKeys(query.UnknownConfigKeysQuery{SDDDir: sddDir})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, s := range result.Ignored {
		got[s.Key] = s.File
		if strings.HasPrefix(s.Reason, s.File) {
			t.Errorf("the reason should not repeat the file it names, got %+v", s)
		}
	}
	if got["verify.driver"] != committedPath {
		t.Errorf("a committed driver should be reported in the committed file, got %+v", result.Ignored)
	}
	if got["verify.mode"] != localPath {
		t.Errorf("a local mode lowering the shared one should be reported in the local file, got %+v", result.Ignored)
	}
}
