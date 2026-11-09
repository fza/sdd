package syncstate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/syncstate"
)

func TestTheGraphLocksIgnoreTheirRuntimeFolder(t *testing.T) {
	locks := []struct {
		name string
		take func(graphDir string) (func(), error)
	}{
		{name: "graph lock", take: func(graphDir string) (func(), error) {
			lock, err := syncstate.LockGraph(graphDir)
			return func() { syncstate.Unlock(lock) }, err
		}},
		{name: "titles lock", take: func(graphDir string) (func(), error) {
			return syncstate.CheckoutLocks{GraphDir: graphDir}.TryLockTitles(t.Context())
		}},
	}
	for _, lock := range locks {
		t.Run(lock.name, func(t *testing.T) {
			graphDir := t.TempDir()
			release, err := lock.take(graphDir)
			if err != nil {
				t.Fatal(err)
			}
			release()
			ignore, err := os.ReadFile(filepath.Join(graphDir, model.GraphRuntimeDir, ".gitignore"))
			if err != nil || string(ignore) != model.GraphRuntimeIgnore {
				t.Errorf("the lock's runtime folder should ignore itself: %v\n%s", err, ignore)
			}
		})
	}
}
