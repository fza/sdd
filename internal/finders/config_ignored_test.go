package finders_test

import (
	"os"
	"path/filepath"
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
