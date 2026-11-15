package index_test

import (
	"os"
	"path/filepath"
	"slices"
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

func TestManifestCapChangeKeepsPreviousCapUntilSettled(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// stored is the cap the manifest records before the fill.
		stored int
		// fill is the cap the fill chunks under.
		fill int
		// settle is the cap the fill settles under, zero for an unfinished fill.
		settle      int
		wantSettled bool
		wantRead    []int
	}{
		{name: "unchanged cap reads one cap", stored: 1792, fill: 1792, wantRead: []int{1792}},
		{name: "changed cap reads both caps", stored: 1792, fill: 1024, wantRead: []int{1024, 1792}},
		{name: "pre-cap store reads uncapped beside the new cap", stored: 0, fill: 1024, wantRead: []int{1024, 0}},
		{name: "settled fill reads the new cap only", stored: 1792, fill: 1024, settle: 1024, wantSettled: true, wantRead: []int{1024}},
		{name: "settling under another cap keeps both", stored: 1792, fill: 1024, settle: 512, wantRead: []int{1024, 1792}},
		{name: "settling with no pending cap changes nothing", stored: 1792, fill: 1792, settle: 1792, wantRead: []int{1792}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			m := &index.Manifest{Version: 1, MaxRunes: tc.stored, Entries: map[string]index.EntryState{}}
			m.SetMaxRunes(tc.fill)
			if err := m.Save(dir); err != nil {
				t.Fatalf("Save: %v", err)
			}
			loaded, err := index.LoadManifest(dir)
			if err != nil {
				t.Fatalf("LoadManifest: %v", err)
			}
			if tc.settle != 0 {
				if got := loaded.SettleMaxRunes(tc.settle); got != tc.wantSettled {
					t.Errorf("settle reported a change = %v, want %v", got, tc.wantSettled)
				}
			}
			if got := loaded.ReadMaxRunes(); !slices.Equal(got, tc.wantRead) {
				t.Errorf("read caps should hold the fill's cap and any pending previous cap, got %v", got)
			}
		})
	}
}
