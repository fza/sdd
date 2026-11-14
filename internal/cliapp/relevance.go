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

// relevanceStore is a local relevance ledger, which can also compact.
type relevanceStore interface {
	sdd.RelevanceStore
	Compact(ctx context.Context) error
}

// projectRelevanceStore opens the project's relevance verdict ledger
// (20261109-004506-d-cpt-udz, 5.7): machine state under stateRoot, keyed
// like the search index, unless relevance.commit keeps it in .sdd/relevance/
// beside sddDir. Under relevance.content: reader only reader verdicts go
// there; scores and drift steps stay in machine state (D14, D20).
func projectRelevanceStore(sddDir, stateRoot, repoKey string, cfg *model.PerRepoConfig) (relevanceStore, error) {
	store, err := projectRelevanceLayout(sddDir, stateRoot, repoKey, cfg)
	if err != nil {
		return nil, err
	}
	used := usedRelevanceStore{relevanceStore: store, stateRoot: stateRoot, repoKey: repoKey}
	if err := used.mark(); err != nil {
		return nil, err
	}
	return used, nil
}

// usedRelevanceStore stamps its project as used on every read and write, so
// the daily sweep keeps a project a long-running server works in
// (20261114-095140-d-cpt-ad4, rule 3).
type usedRelevanceStore struct {
	relevanceStore
	stateRoot, repoKey string
}

func (s usedRelevanceStore) mark() error {
	return localadapter.MarkProjectUsed(s.stateRoot, s.repoKey, time.Now())
}

func (s usedRelevanceStore) Append(ctx context.Context, segment []byte) error {
	if err := s.mark(); err != nil {
		return err
	}
	return s.relevanceStore.Append(ctx, segment)
}

func (s usedRelevanceStore) Segments(ctx context.Context) ([][]byte, error) {
	if err := s.mark(); err != nil {
		return nil, err
	}
	return s.relevanceStore.Segments(ctx)
}

func projectRelevanceLayout(sddDir, stateRoot, repoKey string, cfg *model.PerRepoConfig) (relevanceStore, error) {
	var layout model.RelevanceLayout
	if cfg != nil {
		var err error
		layout, err = cfg.Relevance.Layout()
		if err != nil {
			return nil, err
		}
	}
	local, err := localadapter.NewFilesystemRelevanceStore(filepath.Join(stateRoot, model.RelevanceDirName, filepath.FromSlash(repoKey)))
	if err != nil {
		return nil, err
	}
	if !layout.Commit {
		return local, nil
	}
	committed, err := localadapter.NewFilesystemRelevanceStore(filepath.Join(sddDir, model.RelevanceDirName))
	if err != nil {
		return nil, err
	}
	if layout.Content == model.RelevanceContentAll {
		return committed, nil
	}
	return localadapter.NewSplitRelevanceStore(committed, local)
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
