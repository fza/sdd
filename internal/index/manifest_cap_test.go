package index_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/index"
)

func TestManifestMaxRunesRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		maxRunes int
	}{
		{name: "calibrated cap is kept", maxRunes: 1792},
		{name: "legacy store without a cap reads zero", maxRunes: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			m := &index.Manifest{Version: 1, MaxRunes: tc.maxRunes, Entries: map[string]index.EntryState{}}
			if err := m.Save(dir); err != nil {
				t.Fatalf("Save: %v", err)
			}
			got, err := index.LoadManifest(dir)
			if err != nil {
				t.Fatalf("LoadManifest: %v", err)
			}
			if got.MaxRunes != tc.maxRunes {
				t.Errorf("max runes: got %d, want %d", got.MaxRunes, tc.maxRunes)
			}
		})
	}
}

func TestManifestWithoutCapFieldReadsZero(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"version":1,"entries":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := index.LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if got.MaxRunes != 0 {
		t.Errorf("max runes on a pre-cap manifest: got %d, want 0", got.MaxRunes)
	}
}
