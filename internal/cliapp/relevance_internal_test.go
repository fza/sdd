package cliapp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

// A long-running server opens the ledger once, so each read and write has to
// refresh the project's use stamp a day after the last.
func TestUsedRelevanceStoreRefreshesTheUseStampOnEachUse(t *testing.T) {
	for _, tc := range []struct {
		name string
		use  func(context.Context, usedRelevanceStore) error
	}{
		{"reading segments", func(ctx context.Context, s usedRelevanceStore) error {
			_, err := s.Segments(ctx)
			return err
		}},
		{"appending a segment", func(ctx context.Context, s usedRelevanceStore) error {
			return s.Append(ctx, []byte(`{"verdicts":[]}`))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			const key = "example.test/cli"
			ledger, err := localadapter.NewFilesystemRelevanceStore(filepath.Join(root, model.RelevanceDirName, key))
			if err != nil {
				t.Fatal(err)
			}
			opened := time.Now().UTC().Round(0)
			if err := localadapter.MarkProjectUsed(root, key, opened); err != nil {
				t.Fatal(err)
			}
			used := opened.Add(25 * time.Hour)
			store := usedRelevanceStore{FilesystemRelevanceStore: ledger, stateRoot: root, repoKey: key, now: func() time.Time { return used }}

			if err := tc.use(t.Context(), store); err != nil {
				t.Fatal(err)
			}

			info, err := os.Stat(filepath.Join(root, "used", "example.test", "cli.stamp"))
			if err != nil {
				t.Fatal(err)
			}
			if !info.ModTime().Equal(used) {
				t.Errorf("a use a day after the stamp should refresh it, stamp at %v", info.ModTime())
			}
		})
	}
}
