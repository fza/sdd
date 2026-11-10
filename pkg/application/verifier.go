package application

import (
	"context"
	"fmt"

	"github.com/networkteam/sdd/internal/basefacts"
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
	// Runner is subagent, claude, codex or driver; empty means subagent. The
	// values other than subagent need the composition's DelegateLauncher.
	Runner string
	// Claude and Codex are the model and effort the dispatch asks a Claude or
	// a Codex harness to run a subagent deep verifier with, told apart by the
	// MCP client name; an empty field takes model.DefaultVerifierClaude or
	// model.DefaultVerifierCodex.
	Claude VerifierAgent
	Codex  VerifierAgent
	// Kinds is the verify mode (off, grounding, prose or deep) each entry
	// kind is checked at (model.VerifyConfig.ModeFor); a kind it lacks takes
	// Mode and the grounding GroundingPolicy enforces.
	Kinds map[string]string
	// StricterKinds names, for each kind, the kinds checked more strictly
	// (model.VerifyConfig.StricterKinds): where it names any, pre-flight also
	// tests that the draft fits its kind.
	StricterKinds map[string][]string
}

// verifierFor is the resolved policy a draft of kind is checked under.
func (r *ProjectRuntime) verifierFor(kind string) model.VerifierPolicy {
	policy := r.options.Verifier.resolved()
	if mode, ok := r.options.Verifier.Kinds[kind]; ok {
		_, policy.Mode = model.VerifyMode(mode).Settings()
	}
	return policy
}

// enforcesGrounding reports whether a draft of kind owes a grounding reading.
func (r *ProjectRuntime) enforcesGrounding(kind string) bool {
	if mode, ok := r.options.Verifier.Kinds[kind]; ok {
		enforce, _ := model.VerifyMode(mode).Settings()
		return enforce
	}
	return r.options.Grounding.resolved().Enforce
}

// kindFit is the kind-fit check pre-flight adds for a draft of kind; nil
// where no kind is checked more strictly than it.
func (r *ProjectRuntime) kindFit(kind string) *model.KindFit {
	stricter := r.options.Verifier.StricterKinds[kind]
	if len(stricter) == 0 {
		return nil
	}
	fit := &model.KindFit{Kind: model.Kind(kind), Question: basefacts.KindQuestion(model.Kind(kind))}
	for _, name := range stricter {
		fit.Stricter = append(fit.Stricter, model.KindTest{Kind: model.Kind(name), Question: basefacts.KindQuestion(model.Kind(name))})
	}
	return fit
}

// VerifierAgent is a model and effort a deep verifier runs with.
type VerifierAgent struct {
	Model  string
	Effort string
}

func (p VerifierPolicy) resolved() model.VerifierPolicy {
	return model.VerifierPolicy{
		Mode: model.VerifierMode(p.Mode), Skip: model.VerifierSkip(p.Skip), Runner: model.VerifierRunner(p.Runner),
		Claude: model.VerifierAgent(p.Claude), Codex: model.VerifierAgent(p.Codex),
	}.WithDefaults()
}

// ModelWindow is a model's context window in tokens and the answer reserve
// each call keeps in it.
type ModelWindow struct {
	Tokens        int
	OutputReserve int
}

// lightPreflight builds the prose verifier's pre-flight over reading, sized
// by the runtime's model window; nil when the verifier is off.
func (r *ProjectRuntime) lightPreflight(ctx context.Context, reading *captureReading, kind string) (*query.LightPreflight, error) {
	if r.verifierFor(kind).Mode == model.VerifierOff || reading == nil {
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
