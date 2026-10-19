package index_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/index"
)

func writeManifest(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Loading decodes the entries without EntryState's own decoder; both must read
// every on-disk shape the same way.
func TestLoadManifestReadsEveryShapeAsEntryStateDoes(t *testing.T) {
	content := `{"version": 1, "entries": {
		"flat": {"hash": "h1", "fingerprint": "fp", "chunk_ids": ["flat#summary"], "indexed_at": "2026-10-20T10:00:00Z"},
		"listed": {"versions": [
			{"hash": "h1", "fingerprint": "fp", "derivation": "d1", "chunk_ids": ["listed#v-1#summary"], "indexed_at": "2026-10-19T10:00:00Z"},
			{"hash": "h2", "fingerprint": "fp", "chunk_ids": ["listed#v-2#summary", "listed#v-2#body-0"], "indexed_at": "2026-10-20T10:00:00Z"}
		]},
		"empty": {"versions": []}
	}}`
	loaded, err := index.LoadManifest(writeManifest(t, content))
	if err != nil {
		t.Fatal(err)
	}
	var decoded index.Manifest
	if err := json.Unmarshal([]byte(content), &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Entries, decoded.Entries) {
		t.Errorf("loading should read the entries as EntryState does\nloaded  %+v\ndecoded %+v", loaded.Entries, decoded.Entries)
	}
	want := map[string]int{"flat": 1, "listed": 2, "empty": 0}
	for id, count := range want {
		if got := len(loaded.Entries[id].Versions); got != count {
			t.Errorf("entry %s should hold %d versions, got %d", id, count, got)
		}
	}
	if loaded.Entries["listed"].Versions[1].IndexedAt != time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC) {
		t.Errorf("version fields should be read, got %+v", loaded.Entries["listed"].Versions[1])
	}
}

func TestManifestKeepsAnEntryWithoutVersionsAcrossASave(t *testing.T) {
	dir := t.TempDir()
	manifest := &index.Manifest{Version: 1, Entries: map[string]index.EntryState{"bare": {}}}
	if err := manifest.Save(dir); err != nil {
		t.Fatal(err)
	}
	loaded, err := index.LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Entries["bare"].Versions; len(got) != 0 {
		t.Errorf("an entry saved without versions should load without versions, got %+v", got)
	}
}
