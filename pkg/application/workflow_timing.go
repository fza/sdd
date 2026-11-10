package application

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/internal/textdiff"
)

// The capture's checks run at the point the dialogue mode sets
// (20261109-005108-d-cpt-wcj): guided, the writing guide runs once on the
// content the user approved, then the verifier, and a later adjust gets a
// delta check of its changed lines; autonomous, both run before playback.

// fieldGuideBase records the draft the writing guide's judgment covers: the
// one it judged, or the agent's revision folding its findings in.
const fieldGuideBase = "guideBase"

// guideBase is the value of fieldGuideBase.
type guideBase struct {
	Snapshot string `json:"snapshot"`
	Text     string `json:"text"`
}

// guideDraftFields are the reported fields a delta check compares, the body
// last.
var guideDraftFields = []string{"entryKind", "layer", "refs", "closes", "supersedes", "topics", "participants", "index", "confidence", "intent", "attachments", "canonical", "class", "procedureSpec", "aliases", "actorKind", "roleActor", "involvement", "focusActors", "focusWhen", "body"}

// draftText renders the draft as lines a delta check diffs: one line per
// reported field, then the body.
func draftText(store *engine.Store) string {
	var b strings.Builder
	for _, field := range guideDraftFields {
		value, ok := store.Get(field)
		if !ok || value == nil {
			continue
		}
		if field == "body" {
			text, _ := value.(string)
			b.WriteString("\n" + text)
			continue
		}
		encoded, err := draftJSON(value)
		if err != nil {
			encoded = fmt.Sprintf("%v", value)
		}
		fmt.Fprintf(&b, "%s: %s\n", field, encoded)
	}
	return b.String()
}

// draftJSON keeps <, > and & literal, so an edit quoting the drafter's words
// matches the rendered draft.
func draftJSON(value any) (string, error) {
	var b strings.Builder
	encoder := json.NewEncoder(&b)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(value)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}

func writeGuideBase(store *engine.Store) error {
	return store.WriteEngine(fieldGuideBase, guideBase{Snapshot: store.StateSnapshot(), Text: draftText(store)})
}

func readGuideBase(store *engine.Store) (guideBase, bool) {
	value, ok := store.Get(fieldGuideBase)
	if !ok || value == nil {
		return guideBase{}, false
	}
	doc, ok := value.(map[string]any)
	if !ok {
		return guideBase{}, false
	}
	base := guideBase{}
	base.Snapshot, _ = doc["snapshot"].(string)
	base.Text, _ = doc["text"].(string)
	return base, true
}

func guideRan(store *engine.Store) bool {
	value, ok := store.Get("guideFindings")
	return ok && value != nil
}

func (w *WorkflowSession) verifierMode(instance string) (model.VerifierMode, error) {
	runtime, err := w.targetRuntime(w.instanceProject(instance), AccessRead)
	if err != nil {
		return "", err
	}
	return runtime.options.Verifier.mode(), nil
}

func (w *WorkflowSession) registerWorkflowTiming(registry *engine.Registry) error {
	predicates := []engine.Predicate{
		{
			Doc:      engine.FuncDoc{Name: "autonomousDialogue", Doc: "The session runs in the autonomous dialogue mode, read live from config on every step."},
			Fn:       func(*engine.Context) (bool, error) { return w.Autonomous(), nil },
			Selector: true,
		},
		{
			Doc: engine.FuncDoc{Name: "verifierOn", Doc: "The capture's project sets verify.mode prose or deep, so pre-flight is the prose verifier over the grounding reading."},
			Fn: func(ctx *engine.Context) (bool, error) {
				mode, err := w.verifierMode(ctx.Instance)
				return mode != model.VerifierOff, err
			},
			Selector: true,
		},
		{
			Doc: engine.FuncDoc{Name: "verifyBeforePlayback", Doc: "An autonomous capture with the verifier on: the verifier runs before playback, since no dialogue edits follow."},
			Fn: func(ctx *engine.Context) (bool, error) {
				if !w.Autonomous() {
					return false, nil
				}
				mode, err := w.verifierMode(ctx.Instance)
				return mode != model.VerifierOff, err
			},
			Selector: true,
		},
		{
			Doc:      engine.FuncDoc{Name: "guideDue", Doc: "The writing guide has not judged this capture yet.", Reads: []string{"guideFindings"}},
			Fn:       func(ctx *engine.Context) (bool, error) { return !guideRan(ctx.Store), nil },
			Selector: true,
		},
		{
			Doc: engine.FuncDoc{Name: "guideDeltaDue", Doc: "A guided capture whose draft changed since the writing guide's judgment covered it: the last adjust gets a delta check of its changed lines.", Reads: []string{"guideFindings", fieldGuideBase}},
			Fn: func(ctx *engine.Context) (bool, error) {
				if w.Autonomous() || !guideRan(ctx.Store) {
					return false, nil
				}
				base, ok := readGuideBase(ctx.Store)
				return ok && base.Snapshot != ctx.Store.StateSnapshot(), nil
			},
		},
		{
			Doc: engine.FuncDoc{Name: "quietPreflight", Doc: "The last pre-flight leaves nothing to serve: the verifier is off, or it raised no finding.", Reads: []string{"findings"}},
			Fn: func(ctx *engine.Context) (bool, error) {
				mode, err := w.verifierMode(ctx.Instance)
				if err != nil || mode == model.VerifierOff {
					return true, err
				}
				return len(workflowStoreDocuments(ctx.Store, "findings")) == 0, nil
			},
		},
	}
	for _, predicate := range predicates {
		err := registry.RegisterPredicate(predicate)
		if err != nil {
			return err
		}
	}
	err := registry.RegisterCommand(engine.Command{
		Doc: engine.FuncDoc{
			Name: "recordGuideRevision", Doc: "Records that the agent folded the guide's findings in: the guide step then passes through, and the revised draft is what the guide's judgment covers.",
			Reads: []string{"guideFindings"}, Writes: []string{"guideReviewed", fieldGuideBase},
		},
		Fn: func(ctx *engine.Context) error {
			err := ctx.Store.WriteEngine("guideReviewed", true)
			if err != nil {
				return err
			}
			return writeGuideBase(ctx.Store)
		},
	})
	if err != nil {
		return err
	}
	return registry.RegisterCommand(engine.Command{
		Doc: engine.FuncDoc{
			Name: "writingGuideDelta", Doc: "Runs the writing guide on the lines changed since its judgment last covered the draft, the whole draft as context; findings are drafting input, never a gate.",
			Reads: []string{"body", "entryKind", "layer", "refs", "intent", "attachments", "closes", "supersedes", fieldGuideBase}, Writes: []string{"guideFindings", "guideReviewed", fieldGuideBase},
		},
		Fn: w.runWorkflowWritingGuideDelta,
	})
}

func (w *WorkflowSession) runWorkflowWritingGuideDelta(ctx *engine.Context) error {
	base, _ := readGuideBase(ctx.Store)
	changed := textdiff.ChangedPassages(base.Text, draftText(ctx.Store))
	guideFindings := []query.GuideFinding{}
	if len(changed) > 0 {
		findings, err := w.app.writingGuideCheck(w.ctx, w.identity, w.instanceProject(ctx.Instance), w.draftFromStore(ctx.Store), changed)
		if err != nil {
			return fmt.Errorf("writingGuideDelta: %w", err)
		}
		for _, f := range findings {
			guideFindings = append(guideFindings, query.GuideFinding{Reasoning: f.Reasoning, Axis: f.Axis, Quote: f.Quote, Repair: f.Repair, Severity: query.GuideSeverity(f.Severity)})
		}
	}
	err := ctx.Store.WriteEngine("guideFindings", guideFindings)
	if err != nil {
		return fmt.Errorf("writingGuideDelta: %w", err)
	}
	err = ctx.Store.WriteEngine("guideReviewed", nil)
	if err != nil {
		return fmt.Errorf("writingGuideDelta: %w", err)
	}
	return writeGuideBase(ctx.Store)
}
