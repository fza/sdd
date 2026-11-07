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

func TestUnknownConfigKeysReportsIgnoredSyncMode(t *testing.T) {
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "global.yaml")
	sddDir := filepath.Join(dir, ".sdd")
	files := map[string]string{
		globalPath:                                 "sync:\n  mode: sometimes\n",
		filepath.Join(sddDir, "config.yaml"):       "repo_id: example.com/r\nsync:\n  mode: auto\n",
		filepath.Join(sddDir, "config.local.yaml"): "sync:\n  mode: auto\n",
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
	got := map[string]string{}
	for _, s := range result.Ignored {
		if s.Key != "sync.mode" || s.Reason == "" {
			t.Errorf("an ignored setting should name its key and why, got %+v", s)
		}
		got[s.File] = s.Reason
	}
	if _, ok := got[globalPath]; !ok {
		t.Error("an unknown global value should be reported")
	}
	if _, ok := got[filepath.Join(sddDir, "config.yaml")]; !ok {
		t.Error("a committed mode should be reported")
	}
	if _, ok := got[filepath.Join(sddDir, "config.local.yaml")]; ok {
		t.Error("a local auto should be honored, not reported")
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
