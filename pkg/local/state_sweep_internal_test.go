package local

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

// A process reopening an unused project between the sweep judging it unused
// and a removal keeps what that removal would have taken. Only the seam
// before each removal can stage that reopening.
func TestStateSweepKeepsTheStateOfAProjectReopenedMidSweep(t *testing.T) {
	const key = "github.com/acme/reopened"
	now := time.Now().UTC().Round(0)
	unusedRetention := 90 * 24 * time.Hour
	for _, category := range []string{model.RelevanceDirName, sessionsCategory, stagedBlobsCategory} {
		t.Run(category, func(t *testing.T) {
			root := t.TempDir()
			sweep := StateSweep{StateRoot: root}
			for _, dir := range []string{sweep.dir(model.RelevanceDirName, key), sweep.dir(sessionsCategory, key), filepath.Join(sweep.dir(stagedBlobsCategory, key), "local")} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(sweep.dir(model.RelevanceDirName, key), "ledger.jsonl"), []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := MarkProjectUsed(root, key, now.Add(-unusedRetention-time.Hour)); err != nil {
				t.Fatal(err)
			}
			target := sweep.dir(category, key)
			sweep.beforeRemoval = func(path string) {
				if path != target {
					return
				}
				if err := MarkProjectUsed(root, key, now); err != nil {
					t.Fatal(err)
				}
			}

			if _, err := sweep.Sweep(t.Context(), now, 14*24*time.Hour, unusedRetention); err != nil {
				t.Fatal(err)
			}

			if _, err := os.Stat(target); err != nil {
				t.Errorf("%s of a project reopened before its removal should stay: %v", category, err)
			}
		})
	}
}
