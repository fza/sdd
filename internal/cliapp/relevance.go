package cliapp

import (
	"path/filepath"

	"github.com/networkteam/sdd/internal/llm/factory"
	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"

	localadapter "github.com/networkteam/sdd/pkg/local"
)

// relevanceLedgerDir is the folder of a project's relevance verdict ledger
// (20261109-004506-d-cpt-udz, 5.7): machine state under stateRoot, keyed
// like the search index, and nowhere in the repository
// (20261114-105752-d-tac-tnk).
func relevanceLedgerDir(stateRoot, repoKey string) string {
	return filepath.Join(stateRoot, model.RelevanceDirName, filepath.FromSlash(repoKey))
}

const leftoverRelevanceNotice = "ignoring relevance segments in the repository: the ledger lives only in machine state"

func leftoverRelevanceAdvice(ledgerDir string) string {
	return "move them into " + ledgerDir + ", then commit their removal from the repository"
}

func projectRelevanceStore(stateRoot, repoKey string) (*localadapter.FilesystemRelevanceStore, error) {
	return localadapter.NewFilesystemRelevanceStore(relevanceLedgerDir(stateRoot, repoKey))
}

// projectPrefilter composes the prefilter verify.prefilter switches on;
// the zero value while it is off (D7).
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
