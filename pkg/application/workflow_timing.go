package application

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/internal/serveview"
	"github.com/networkteam/sdd/internal/textdiff"
)

// The capture's checks run at the point the dialogue mode sets: guided, the
// writing guide runs once on the content the user approved, then the
// verifier, and a later adjust gets a delta check of its changed lines;
// autonomous, both run before playback.

// fieldGuideBase records the draft the writing guide's judgment covers: the
// one it judged, or the agent's revision folding its findings in.
const fieldGuideBase = "guideBase"

// guideBase is the value of fieldGuideBase.
type guideBase struct {
	Snapshot string `json:"snapshot"`
	Text     string `json:"text"`
	Body     string `json:"body"`
}

// fieldGuideAdded records the sentences a guide revision added to the body
// and removed from it, listed at playback until the user confirms a draft
// after it.
const fieldGuideAdded = "guideAdded"

// guideAdded is the value of fieldGuideAdded. Confirmation is the playback
// confirmation that stood at the revision, so a later confirm ends the lists.
type guideAdded struct {
	Confirmation string   `json:"confirmation"`
	Sentences    []string `json:"sentences"`
	Removed      []string `json:"removed"`
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
	body, _ := workflowStoreString(store, fieldBody)
	return store.WriteEngine(fieldGuideBase, guideBase{Snapshot: store.StateSnapshot(), Text: draftText(store), Body: body})
}

func recordGuideAdded(store *engine.Store) error {
	base, _ := readGuideBase(store)
	body, _ := workflowStoreString(store, fieldBody)
	confirmation, err := playbackConfirmationKey(store)
	if err != nil {
		return err
	}
	return store.WriteEngine(fieldGuideAdded, guideAdded{
		Confirmation: confirmation,
		Sentences:    textdiff.AddedSentences(base.Body, body),
		Removed:      textdiff.AddedSentences(body, base.Body),
	})
}

func playbackConfirmationKey(store *engine.Store) (string, error) {
	value, ok := store.Get("playbackConfirmation")
	if !ok || value == nil {
		return "", nil
	}
	return draftJSON(value)
}

// guideRevisionList renders the sentences recorded under key that the body
// still holds, or with held false still lacks, one quoted bullet each; empty
// once a confirm followed the revision.
func guideRevisionList(store *engine.Store, key string, held bool) (string, error) {
	doc, ok := workflowStoreDocument(store, fieldGuideAdded)
	if !ok {
		return "", nil
	}
	confirmation, err := playbackConfirmationKey(store)
	if err != nil {
		return "", err
	}
	if recorded, _ := doc["confirmation"].(string); recorded != confirmation {
		return "", nil
	}
	body, _ := workflowStoreString(store, fieldBody)
	body = strings.Join(strings.Fields(body), " ")
	var b strings.Builder
	sentences, _ := doc[key].([]any)
	for _, item := range sentences {
		sentence, _ := item.(string)
		if sentence != "" && strings.Contains(body, sentence) == held {
			b.WriteString("- \"" + sentence + "\"\n")
		}
	}
	return b.String(), nil
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
	base.Body, _ = doc["body"].(string)
	return base, true
}

func guideRan(store *engine.Store) bool {
	value, ok := store.Get("guideFindings")
	return ok && value != nil
}

func (w *WorkflowSession) verifierMode(ctx *engine.Context) (model.VerifierMode, error) {
	policy, _, err := w.deepPolicy(ctx.Instance, ctx.Store)
	return policy.Mode, err
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
				mode, err := w.verifierMode(ctx)
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
				mode, err := w.verifierMode(ctx)
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
				mode, err := w.verifierMode(ctx)
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
			Name: "recordGuideRevision", Doc: "Records that the agent folded the guide's findings in: the guide step then passes through, and the revised draft is what the guide's judgment covers, and the sentences it added to the body and removed from it are recorded for playback.",
			Reads: []string{"guideFindings", fieldGuideBase}, Writes: []string{"guideReviewed", fieldGuideBase, fieldGuideAdded},
		},
		Fn: func(ctx *engine.Context) error {
			err := ctx.Store.WriteEngine("guideReviewed", true)
			if err != nil {
				return err
			}
			err = recordGuideAdded(ctx.Store)
			if err != nil {
				return err
			}
			return writeGuideBase(ctx.Store)
		},
	})
	if err != nil {
		return err
	}
	// The added and the removed list share one text allowance, so the
	// playback's worst case stays within the capture's serve budget.
	listBound := engine.QueryBound{Part: serveview.PartText, Cap: serveview.Cap{MaxBytes: serveview.Default().Cap(serveview.PartText).MaxBytes / 2}}
	err = registry.RegisterQuery(engine.Query{
		Doc:       engine.FuncDoc{Name: "guideAdded", Doc: "The sentences the last guide revision added to the body, quoted one per line, until the user confirms a draft after it.", Reads: []string{fieldGuideAdded, "body"}},
		ServeSafe: true,
		Bound:     listBound,
		Fn: func(ctx *engine.Context, _ map[string]any) (any, error) {
			return guideRevisionList(ctx.Store, "sentences", true)
		},
	})
	if err != nil {
		return err
	}
	err = registry.RegisterQuery(engine.Query{
		Doc:       engine.FuncDoc{Name: "guideRemoved", Doc: "The sentences the last guide revision removed from the body, quoted one per line, until the user confirms a draft after it.", Reads: []string{fieldGuideAdded, "body"}},
		ServeSafe: true,
		Bound:     listBound,
		Fn: func(ctx *engine.Context, _ map[string]any) (any, error) {
			return guideRevisionList(ctx.Store, "removed", false)
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
	text := draftText(ctx.Store)
	changed := textdiff.ChangedPassages(base.Text, text)
	guideFindings := []query.GuideFinding{}
	if len(changed) > 0 {
		findings, err := w.app.writingGuideCheck(w.ctx, w.identity, w.instanceProject(ctx.Instance), w.draftFromStore(ctx.Store), changed)
		if err != nil {
			return fmt.Errorf("writingGuideDelta: %w", err)
		}
		for _, f := range findings {
			// The model sees the whole draft as context; what it judges is the change.
			if !llmops.QuoteLocatesInAny(text, f.Quote, changed) {
				continue
			}
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
