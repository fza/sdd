package cliapp

import (
	"context"
	"path/filepath"

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
