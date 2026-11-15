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
	if _, err := os.Stat(filepath.Join(f.root, "escaped")); !os.IsNotExist(err) {
		t.Errorf("index gc should create nothing outside the cache root: %v", err)
	}
}
