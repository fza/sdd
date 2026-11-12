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

func TestUnknownConfigKeysReportsTheRetiredTokenSavingAsIgnored(t *testing.T) {
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "global.yaml")
	sddDir := filepath.Join(dir, ".sdd")
	files := map[string]string{
		globalPath:                                 "sessions:\n  token_saving: off\n",
		filepath.Join(sddDir, "config.yaml"):       "repo_id: example.com/r\nsessions:\n  token_saving: off\n",
		filepath.Join(sddDir, "config.local.yaml"): "sessions:\n  token_saving: off\n",
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
	reported := map[string]bool{}
	for _, s := range result.Ignored {
		if s.Key == "sessions.token_saving" && strings.Contains(s.Reason, "no longer used") {
			reported[s.File] = true
		}
	}
	for path := range files {
		if !reported[path] {
			t.Errorf("every layer naming the retired key should report it as ignored: %s", path)
		}
	}
	for _, k := range result.Keys {
		if k.Key == "sessions.token_saving" {
			t.Errorf("the retired key should read as an ignored setting, not an unknown key: %s", k.File)
		}
	}
}
