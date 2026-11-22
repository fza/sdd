package finders_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/internal/repos"
)

// A config that still sets verify.brief_all_below loads, and sdd shows the
// retired key as ignored in every layer.
func TestUnknownConfigKeysReportsTheRetiredBriefAllBelowAsIgnored(t *testing.T) {
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "global.yaml")
	sddDir := filepath.Join(dir, ".sdd")
	files := map[string]string{
		globalPath:                                 "verify:\n  brief_all_below: 200\n",
		filepath.Join(sddDir, "config.yaml"):       "repo_id: example.com/r\nverify:\n  brief_all_below: 200\n",
		filepath.Join(sddDir, "config.local.yaml"): "verify:\n  brief_all_below: 200\n",
	}
	writeFiles(t, files)
	_, err := meta.ReadConfig(sddDir, "")
	if err != nil {
		t.Fatalf("a config holding the retired key should load, got %v", err)
	}
	f := finders.New(finders.Options{Repos: repos.NewRegistry(repos.Locations{ConfigPath: globalPath, CacheRoot: filepath.Join(dir, "cache")})})

	result, err := f.UnknownConfigKeys(query.UnknownConfigKeysQuery{SDDDir: sddDir})
	if err != nil {
		t.Fatal(err)
	}
	reported := map[string]bool{}
	for _, s := range result.Ignored {
		if s.Key == "verify.brief_all_below" && strings.Contains(s.Reason, "no longer used") {
			reported[s.File] = true
		}
	}
	for path := range files {
		if !reported[path] {
			t.Errorf("every layer naming the retired key should report it as ignored: %s", path)
		}
	}
	for _, k := range result.Keys {
		if k.Key == "verify.brief_all_below" {
			t.Errorf("the retired key should read as an ignored setting, not an unknown key: %s", k.File)
		}
	}
}
