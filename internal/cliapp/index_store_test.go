package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndexStoreRefusesAnInvalidRepoID(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/../../../escaped\ngraph_dir: decisions\ndefault_branch: main\n")

	result := f.run(t, "index", "gc", "--embedding-provider", "ollama", "--embedding-model", "test-model", "--embedding-ollama-endpoint", "http://127.0.0.1:9")
	if result.err == nil || !strings.Contains(result.err.Error(), "repo_id") {
		t.Errorf("index gc should refuse the repo_id, got %v", result.err)
	}
	for _, escaped := range []string{filepath.Join(f.root, "escaped"), filepath.Join(f.root, "cache", "escaped")} {
		if _, err := os.Stat(escaped); !os.IsNotExist(err) {
			t.Errorf("index gc should create nothing outside the index root: %v", err)
		}
	}
}
