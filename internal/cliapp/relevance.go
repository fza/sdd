package cliapp

import (
	"path/filepath"

	localadapter "github.com/networkteam/sdd/pkg/local"
)

// projectRelevanceStore opens the project's relevance verdict ledger: by
// default machine state under stateRoot, keyed like the search index
// (20261109-004506-d-cpt-udz, 5.7).
func projectRelevanceStore(stateRoot, repoKey string) (*localadapter.FilesystemRelevanceStore, error) {
	return localadapter.NewFilesystemRelevanceStore(filepath.Join(stateRoot, "relevance", filepath.FromSlash(repoKey)))
}
