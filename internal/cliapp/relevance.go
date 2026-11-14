package cliapp

import (
	"context"
	"path/filepath"
	"time"

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

// projectRelevanceStore opens the project's relevance verdict ledger and
// stamps the project as used.
func projectRelevanceStore(stateRoot, repoKey string) (usedRelevanceStore, error) {
	store, err := localadapter.NewFilesystemRelevanceStore(relevanceLedgerDir(stateRoot, repoKey))
	if err != nil {
		return usedRelevanceStore{}, err
	}
	used := usedRelevanceStore{FilesystemRelevanceStore: store, stateRoot: stateRoot, repoKey: repoKey}
	if err := used.mark(); err != nil {
		return usedRelevanceStore{}, err
	}
	return used, nil
}

// usedRelevanceStore stamps its project as used on every read and write, so
// the daily sweep keeps a project a long-running server works in
// (20261114-095140-d-cpt-ad4, rule 3).
type usedRelevanceStore struct {
	*localadapter.FilesystemRelevanceStore
	stateRoot, repoKey string
}

func (s usedRelevanceStore) mark() error {
	return localadapter.MarkProjectUsed(s.stateRoot, s.repoKey, time.Now())
}

func (s usedRelevanceStore) Append(ctx context.Context, segment []byte) error {
	if err := s.mark(); err != nil {
		return err
	}
	return s.FilesystemRelevanceStore.Append(ctx, segment)
}

func (s usedRelevanceStore) Segments(ctx context.Context) ([][]byte, error) {
	if err := s.mark(); err != nil {
		return nil, err
	}
	return s.FilesystemRelevanceStore.Segments(ctx)
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
