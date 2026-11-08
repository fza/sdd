package local_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/local"
)

func TestFilesystemGraphStoreIgnoresItsRuntimeFolder(t *testing.T) {
	graphDir := filepath.Join(t.TempDir(), "graph")
	_, err := local.NewFilesystemGraphStore(local.FilesystemGraphStoreOptions{Project: "example", GraphDir: graphDir})
	if err != nil {
		t.Fatal(err)
	}
	ignore, err := os.ReadFile(filepath.Join(graphDir, model.GraphRuntimeDir, ".gitignore"))
	if err != nil || string(ignore) != model.GraphRuntimeIgnore {
		t.Errorf("the store's runtime folder should ignore itself: %v\n%s", err, ignore)
	}
}
