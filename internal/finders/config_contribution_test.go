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

func TestUnknownConfigKeysReportsTheRetiredContributionAsIgnored(t *testing.T) {
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "global.yaml")
	sddDir := filepath.Join(dir, ".sdd")
	files := map[string]string{
		globalPath:                                 "",
		filepath.Join(sddDir, "config.yaml"):       "repo_id: example.com/r\ncontribution: origin/main\n",
		filepath.Join(sddDir, "config.local.yaml"): "contribution: origin/main\n",
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
		if s.Key == "contribution" && strings.Contains(s.Reason, "no longer used: contribution mode was removed") {
			reported[s.File] = true
		}
	}
	for _, path := range []string{filepath.Join(sddDir, "config.yaml"), filepath.Join(sddDir, "config.local.yaml")} {
		if !reported[path] {
			t.Errorf("a layer naming the retired key should report it as ignored with its reason: %s", path)
		}
	}
	for _, k := range result.Keys {
		if k.Key == "contribution" {
			t.Errorf("the retired key should read as an ignored setting, not an unknown key: %s", k.File)
		}
	}
}
