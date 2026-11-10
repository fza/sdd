package model

import (
	"fmt"
	"slices"
)

// RelevanceDirName is the directory under .sdd/ holding the committed copy
// of the relevance ledger (20261109-004506-d-cpt-udz, D17 and D18).
const RelevanceDirName = "relevance"

// RelevanceContent is what the committed ledger holds (D20).
type RelevanceContent string

const (
	// RelevanceContentAll commits reader verdicts, filter scores and drift
	// steps.
	RelevanceContentAll RelevanceContent = "all"
	// RelevanceContentReader commits reader verdicts only; scores and drift
	// steps stay in machine state.
	RelevanceContentReader RelevanceContent = "reader"
)

func (RelevanceContent) AllowedValues() []string {
	return closedValues(RelevanceContentAll, RelevanceContentReader)
}

// RelevanceConfig is the `relevance` block. Only the committed
// .sdd/config.yaml sets it, since what the repository carries is the team's
// choice (D14, D20).
type RelevanceConfig struct {
	// Commit keeps the ledger in .sdd/relevance/, where its segments ride the
	// next sdd auto-commit; off, it lives in machine state.
	Commit *bool `yaml:"commit,omitempty"`
	// Content is what the committed ledger holds, default all.
	Content RelevanceContent `yaml:"content,omitempty"`
}

// RelevanceLayout is where the ledger's records live.
type RelevanceLayout struct {
	Commit  bool
	Content RelevanceContent
}

// Layout resolves the block to its defaults and checks the content value.
func (c RelevanceConfig) Layout() (RelevanceLayout, error) {
	layout := RelevanceLayout{Commit: c.Commit != nil && *c.Commit, Content: c.Content}
	if c.Content == "" {
		layout.Content = RelevanceContentAll
	} else if !slices.Contains(c.Content.AllowedValues(), string(c.Content)) {
		return RelevanceLayout{}, fmt.Errorf("relevance.content: unknown value %q (%s)", c.Content, oneOf(c.Content.AllowedValues()))
	}
	return layout, nil
}

func mergeRelevanceConfig(base, overlay RelevanceConfig) RelevanceConfig {
	out := base
	if overlay.Commit != nil {
		out.Commit = overlay.Commit
	}
	if overlay.Content != "" {
		out.Content = overlay.Content
	}
	return out
}
