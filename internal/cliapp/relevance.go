package cliapp

import (
	"path/filepath"

	"github.com/networkteam/sdd/internal/llm/factory"
	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"

	localadapter "github.com/networkteam/sdd/pkg/local"
)

// relevanceLedgerDir is the folder of a project's relevance verdict ledger:
// machine state under stateRoot, keyed like the search index, and nowhere in
// the repository.
func relevanceLedgerDir(stateRoot, repoKey string) string {
	return filepath.Join(stateRoot, model.RelevanceDirName, filepath.FromSlash(repoKey))
}

// projectRelevanceStore opens the project's relevance verdict ledger.
func projectRelevanceStore(stateRoot, repoKey string) (*localadapter.FilesystemRelevanceStore, error) {
	return localadapter.NewFilesystemRelevanceStore(relevanceLedgerDir(stateRoot, repoKey))
}

// projectPrefilter composes the prefilter verify.prefilter switches on;
// the zero value while it is off.
func projectPrefilter(cfg *model.PerRepoConfig) (sdd.Prefilter, error) {
	var base model.BaseConfig
	if cfg != nil {
		base = cfg.BaseConfig
	}
	if !base.Verify.GroundingPolicy().Prefilter {
		return sdd.Prefilter{}, nil
	}
	prefilter, err := factory.NewPrefilter(base.ResolvedPrefilter(), statsSink())
	if err != nil {
		return sdd.Prefilter{}, err
	}
	return sdd.Prefilter{Scorer: prefilter.Scorer, Judge: prefilter.Judge}, nil
}
