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

func writeFiles(t *testing.T, files map[string]string) {
	t.Helper()
	for path, content := range files {
		err := os.MkdirAll(filepath.Dir(path), 0o755)
		if err != nil {
			t.Fatal(err)
		}
		err = os.WriteFile(path, []byte(content), 0o644)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestUnknownConfigKeysReportsTheRetiredRelevanceKeysAsIgnored(t *testing.T) {
	dir := t.TempDir()
	sddDir := filepath.Join(dir, ".sdd")
	files := map[string]string{
		filepath.Join(sddDir, "config.yaml"):       "repo_id: example.com/r\nrelevance:\n  commit: true\n  content: reader\n",
		filepath.Join(sddDir, "config.local.yaml"): "relevance:\n  commit: true\n  content: reader\n",
	}
	writeFiles(t, files)
	f := finders.New(finders.Options{Repos: repos.NewRegistry(repos.Locations{ConfigPath: filepath.Join(dir, "global.yaml"), CacheRoot: filepath.Join(dir, "cache")})})

	result, err := f.UnknownConfigKeys(query.UnknownConfigKeysQuery{SDDDir: sddDir})
	if err != nil {
		t.Fatal(err)
	}
	reported := map[string]bool{}
	for _, s := range result.Ignored {
		if strings.Contains(s.Reason, "no longer used") && strings.Contains(s.Reason, "machine state") {
			reported[s.File+" "+s.Key] = true
		}
	}
	for path := range files {
		for _, key := range []string{"relevance.commit", "relevance.content"} {
			if !reported[path+" "+key] {
				t.Errorf("every layer naming a retired relevance key should report it as ignored: %s in %s", key, path)
			}
		}
	}
	for _, k := range result.Keys {
		if strings.HasPrefix(k.Key, "relevance") {
			t.Errorf("a retired relevance key should read as an ignored setting, not an unknown key: %s in %s", k.Key, k.File)
		}
	}
}

func TestLeftoverRelevanceListsTheSegmentsUnderSDD(t *testing.T) {
	sddDir := filepath.Join(t.TempDir(), ".sdd")
	dir := filepath.Join(sddDir, "relevance")
	writeFiles(t, map[string]string{
		filepath.Join(dir, "a.seg"):     "x",
		filepath.Join(dir, "b.seg"):     "x",
		filepath.Join(dir, "notes.txt"): "x",
	})
	f := finders.New(finders.Options{})

	result, err := f.LeftoverRelevance(query.LeftoverRelevanceQuery{SDDDir: sddDir})
	if err != nil {
		t.Fatal(err)
	}
	if result.Dir != dir || len(result.Segments) != 2 {
		t.Fatalf("the segments under .sdd/relevance/ should be listed, and nothing else: %+v", result)
	}

	none, err := f.LeftoverRelevance(query.LeftoverRelevanceQuery{SDDDir: filepath.Join(t.TempDir(), ".sdd")})
	if err != nil || len(none.Segments) != 0 {
		t.Fatalf("a project without the folder should report nothing, got %+v, %v", none, err)
	}
}
