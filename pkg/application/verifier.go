package application

import (
	"context"
	"fmt"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

// VerifierPolicy is how far the draft verifier checks a capture against its
// grounding reading (20261109-004729-d-cpt-xs5).
type VerifierPolicy struct {
	// Mode is off, prose or deep; empty means off.
	Mode string
	// Skip is never, failure or weighed; empty means failure.
	Skip string
	// Launch is subagent, claude, codex or custom_driver; empty means
	// subagent. The values other than subagent need the composition's
	// DelegateLauncher.
	Launch string
	// Subagent is the model and effort the dispatch asks the harness to run a
	// subagent-launched deep verifier with; empty leaves the harness's own.
	Subagent VerifierAgent
}

// VerifierAgent is a model and effort a deep verifier launch runs with.
type VerifierAgent struct {
	Model  string
	Effort string
}

func (p VerifierPolicy) resolved() model.VerifierPolicy {
	return model.VerifierPolicy{Mode: model.VerifierMode(p.Mode), Skip: model.VerifierSkip(p.Skip), Launch: model.VerifierLaunch(p.Launch)}.WithDefaults()
}

func (p VerifierPolicy) mode() model.VerifierMode { return p.resolved().Mode }

// ModelWindow is a model's context window in tokens and the answer reserve
// each call keeps in it.
type ModelWindow struct {
	Tokens        int
	OutputReserve int
}

// lightPreflight builds the prose verifier's pre-flight over reading, sized
// by the runtime's model window; nil when the verifier is off.
func (r *ProjectRuntime) lightPreflight(ctx context.Context, reading *captureReading) (*query.LightPreflight, error) {
	if r.options.Verifier.mode() == model.VerifierOff || reading == nil {
		return nil, nil
	}
	if r.options.Window == nil {
		return nil, fmt.Errorf("the prose verifier needs the model's context window: set llm.context_tokens")
	}
	window, err := r.options.Window(ctx)
	if err != nil {
		return nil, err
	}
	budget := model.LightBudgetChars(window.Tokens, window.OutputReserve)
	if budget <= 0 {
		return nil, fmt.Errorf("the model's %d-token window leaves no room beside the %d-token answer reserve: raise llm.context_tokens or lower llm.max_tokens", window.Tokens, window.OutputReserve)
	}
	return &query.LightPreflight{Full: reading.full, Listed: reading.listed, SetAside: reading.setAside, BudgetChars: budget}, nil
}

// captureReading is the grounding reading of one capture instance as a light
// pre-flight carries it.
type captureReading struct {
	full     []string
	listed   []string
	setAside []model.GroundingVerdict
}
