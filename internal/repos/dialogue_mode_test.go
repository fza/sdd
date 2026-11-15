package repos_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/repos"
)

func TestLoadConfigFromRefusesAnUnknownDialogueMode(t *testing.T) {
	for _, mode := range []string{"autonomous", "free"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("dialogue:\n  mode: "+mode+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			_, err := repos.LoadConfigFrom(path)
			if err == nil {
				t.Fatal("an unknown dialogue mode should stop the load")
			}
			for _, part := range []string{path, mode, "guided or auto"} {
				if !strings.Contains(err.Error(), part) {
					t.Errorf("the refusal should name the file, the value and the valid values, missing %q: %v", part, err)
				}
			}
		})
	}
}
