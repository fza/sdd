package cliapp

import (
	"path/filepath"

	"github.com/networkteam/sdd/internal/llm/factory"
	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"

	localadapter "github.com/networkteam/sdd/pkg/local"
)

// projectRelevanceStore opens the project's relevance verdict ledger: by
// default machine state under stateRoot, keyed like the search index
// (20261109-004506-d-cpt-udz, 5.7).
func projectRelevanceStore(stateRoot, repoKey string) (*localadapter.FilesystemRelevanceStore, error) {
	return localadapter.NewFilesystemRelevanceStore(filepath.Join(stateRoot, "relevance", filepath.FromSlash(repoKey)))
}

// projectPrefilter composes the prefilter grounding.prefilter switches on;
// the zero value while it is off (D7).
func projectPrefilter(cfg *model.PerRepoConfig) (sdd.Prefilter, error) {
	if cfg == nil || !cfg.Grounding.Policy().Prefilter {
		return sdd.Prefilter{}, nil
	}
	prefilter, err := factory.NewPrefilter(cfg.LLMPrefilter, statsSink())
	if err != nil {
		return sdd.Prefilter{}, err
	}
	return sdd.Prefilter{Scorer: prefilter.Scorer, Judge: prefilter.Judge}, nil
}
