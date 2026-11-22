package local_test

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/relevance"
	"github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/local"
)

// Each store keeps what it was given: appended segments fold back to their
// records, across compactions of the filesystem store.
func TestProofStores_KeepTheirRecords(t *testing.T) {
	for _, tc := range []struct {
		name  string
		store func(*testing.T) application.ProofStore
	}{
		{name: "filesystem", store: func(t *testing.T) application.ProofStore {
			store, err := local.NewFilesystemProofStore(filepath.Join(t.TempDir(), "proofs"))
			if err != nil {
				t.Fatal(err)
			}
			return store
		}},
		{name: "memory", store: func(*testing.T) application.ProofStore { return local.NewMemoryProofStore() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := tc.store(t)
			segments, err := store.Segments(t.Context())
			if err != nil || len(segments) != 0 {
				t.Fatalf("segments %d, %v: a new store holds none", len(segments), err)
			}
			var want model.ProofRecords
			for i := range 40 {
				records := model.ProofRecords{Misses: []model.Miss{{Capture: string(rune('a' + i%26)), Hidden: "h", Rule: model.RuleThreshold, Detector: model.DetectRescue, Judge: "j", At: time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC)}}}
				want.Misses = append(want.Misses, records.Misses...)
				segment, err := relevance.EncodeProofs(records)
				if err != nil {
					t.Fatal(err)
				}
				if err := store.Append(t.Context(), segment); err != nil {
					t.Fatal(err)
				}
			}
			segments, err = store.Segments(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			got, err := relevance.FoldProofs(segments)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("folded %d misses: every appended record should come back once", len(got.Misses))
			}
		})
	}
}

func TestNewFilesystemProofStore_NeedsADirectory(t *testing.T) {
	if _, err := local.NewFilesystemProofStore(""); err == nil {
		t.Fatal("a store without a directory should be refused")
	}
}
