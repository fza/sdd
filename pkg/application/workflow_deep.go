package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"syscall"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/serveview"
	"github.com/networkteam/sdd/internal/textdiff"
	"github.com/networkteam/sdd/internal/truncate"
)

// The deep draft verifier: once the light pre-flight leaves no high finding
// open, the capture dispatches the verify task into a child session,
// exchanges findings, answers and responses with it for at most
// model.DeepRounds deliveries, and records a deep verdict over the draft it
// settled; the write requires that verdict to be current.

const verifyProcedure = "verify"

// verifyReadStep is the verify step that serves the current page of the reading.
const verifyReadStep = "read"

// Engine-written fields of the capture's deep exchange.
const (
	fieldDeepVerdict    = "deepVerdict"
	fieldDeepBase       = "deepBase"
	fieldDeepReading    = "deepReading"
	fieldDeepWeighing   = "deepWeighing"
	fieldDeepHistory    = "deepHistory"
	fieldDeepRounds     = "deepRounds"
	fieldDeepProposal   = "deepProposal"
	fieldDeepHunks      = "deepHunks"
	fieldDeepOpen       = "deepOpen"
	fieldDeepWithdrawn  = "deepWithdrawn"
	fieldDeepUnedited   = "deepUnedited"
	fieldDeepReplayed   = "deepReplayed"
	fieldDeepListed     = "deepListed"
	fieldDeepUserListed = "deepUserListed"
	fieldDeepProblems   = "deepProblems"
	fieldDeepNoSubagent = "deepNoSubagent"
	fieldDeepForkBase   = "deepForkBase"
	fieldDeepEndReason  = "deepEndReason"
	// fieldPreflightRubric is what pre-flight's model would have judged a
	// draft by, set while the deep verifier judges in its place.
	fieldPreflightRubric = "preflightRubric"
)

// Received and reported fields of the capture's deep exchange.
const (
	fieldDeepFindings  = "deepFindings"
	fieldDeepResponses = "deepResponses"
	fieldDeepContest   = "deepContest"
)

// deepVerdict is the deep verifier's verdict, or the user's skip, over the
// capture input it covers.
type deepVerdict struct {
	Fingerprint string `json:"fingerprint"`
	// Document is the draft document the verdict covers, which a later
	// exchange's delta reading compares against.
	Document string `json:"document"`
	// Outcome is verified, held (its open points wait for the person at
	// playback), settled (by the user after the rounds) or skipped.
	Outcome string `json:"outcome"`
	Skip    string `json:"skip,omitempty"`
	Rounds  int    `json:"rounds"`
	Runner  string `json:"runner"`
}

func readDeepVerdict(store *engine.Store) (deepVerdict, bool) {
	value, ok := store.Get(fieldDeepVerdict)
	if !ok || value == nil {
		return deepVerdict{}, false
	}
	var verdict deepVerdict
	return verdict, decodeStoreValue(value, &verdict) == nil
}

func decodeStoreValue(value, into any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, into)
}

func storeDeep[T any](store *engine.Store, name string) ([]T, error) {
	value, ok := store.Get(name)
	if !ok || value == nil {
		return nil, nil
	}
	var out []T
	err := decodeStoreValue(value, &out)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", name, err)
	}
	return out, nil
}

// deepFingerprint keys the capture input a deep verdict covers: the draft
// document, the staged bytes of its attachments, and the write target.
func (w *WorkflowSession) deepFingerprint(ctx *engine.Context) string {
	return w.deepFingerprintOf(ctx, draftText(ctx.Store))
}

func (w *WorkflowSession) deepFingerprintOf(ctx *engine.Context, document string) string {
	sum := sha256.New()
	sum.Write([]byte(document))
	for _, handle := range workflowStoreStrings(ctx.Store, "attachments") {
		fmt.Fprintf(sum, "\x00%s=%s", handle, w.staged[handle])
	}
	target, _ := w.effectiveTargetFor(w.instanceProject(ctx.Instance), ctx.Store)
	fmt.Fprintf(sum, "\x00%s\x00%s", target.Project, target.Branch)
	return hex.EncodeToString(sum.Sum(nil))
}

// deepPolicy is the verifier policy of the draft's kind in the capture's
// project.
func (w *WorkflowSession) deepPolicy(instance string, store *engine.Store) (model.VerifierPolicy, *ProjectRuntime, error) {
	runtime, err := w.targetRuntime(w.instanceProject(instance), AccessRead)
	if err != nil {
		return model.VerifierPolicy{}, nil, err
	}
	kind, _ := workflowStoreString(store, "entryKind")
	return runtime.verifierFor(kind), runtime, nil
}

// deepVerdictCurrent reports whether the write may go ahead as far as the
// deep verifier is concerned: the project runs no deep verifier, or a
// verdict or skip covers the current input.
func (w *WorkflowSession) deepVerdictCurrent(ctx *engine.Context) (bool, error) {
	policy, _, err := w.deepPolicy(ctx.Instance, ctx.Store)
	if err != nil || policy.Mode != model.VerifierDeep {
		return true, err
	}
	verdict, ok := readDeepVerdict(ctx.Store)
	return ok && verdict.Fingerprint == w.deepFingerprint(ctx), nil
}

const deepOutcomeHeld = "held"

// deepHeld reports whether the current input's deep verdict holds open points
// for the person.
func (w *WorkflowSession) deepHeld(ctx *engine.Context) (bool, error) {
	verdict, ok := readDeepVerdict(ctx.Store)
	return ok && verdict.Outcome == deepOutcomeHeld && verdict.Fingerprint == w.deepFingerprint(ctx), nil
}

// deepEditSettled reports whether a guided capture's draft changed since the
// deep verdict only by reverting or restoring the last exchange's edits
// (model.DeepEditSettled), its attachments and target as they were.
func (w *WorkflowSession) deepEditSettled(ctx *engine.Context) (bool, error) {
	verdict, ok := readDeepVerdict(ctx.Store)
	if w.Autonomous() || !ok || verdict.Outcome == "skipped" || verdict.Fingerprint == w.deepFingerprint(ctx) || verdict.Fingerprint != w.deepFingerprintOf(ctx, verdict.Document) {
		return false, nil
	}
	findings, err := storeDeep[model.DeepFinding](ctx.Store, fieldDeepFindings)
	if err != nil {
		return false, err
	}
	history, err := storeDeep[model.DeepResponse](ctx.Store, fieldDeepHistory)
	if err != nil {
		return false, err
	}
	base, _ := workflowStoreString(ctx.Store, fieldDeepBase)
	return model.DeepEditSettled(base, model.DeepChanges(findings, history), verdict.Document, draftText(ctx.Store)), nil
}

// deepJudgesPreflight reports whether the deep verifier judges the draft in
// pre-flight's place, so pre-flight runs only its mechanical checks: the
// project runs deep, and no skip of the deep exchange covers the current
// draft. A skipped exchange falls back to pre-flight's model, so deep never
// checks less than prose.
func (w *WorkflowSession) deepJudgesPreflight(ctx *engine.Context) (bool, error) {
	policy, _, err := w.deepPolicy(ctx.Instance, ctx.Store)
	if err != nil || policy.Mode != model.VerifierDeep {
		return false, err
	}
	verdict, ok := readDeepVerdict(ctx.Store)
	skipped := ok && verdict.Outcome == "skipped" && verdict.Fingerprint == w.deepFingerprint(ctx)
	return !skipped, nil
}

func (w *WorkflowSession) writeDeepVerdict(ctx *engine.Context, outcome, skip string) error {
	rounds, _ := ctx.Store.Get(fieldDeepRounds)
	count, _ := rounds.(float64)
	if n, ok := rounds.(int); ok {
		count = float64(n)
	}
	policy, _, err := w.deepPolicy(ctx.Instance, ctx.Store)
	if err != nil {
		return err
	}
	runner := string(policy.Runner)
	if fallback, _ := ctx.Store.Get(fieldDeepNoSubagent); fallback == true {
		runner = DelegateLaunchFallback
	}
	return ctx.Store.WriteEngine(fieldDeepVerdict, deepVerdict{
		Fingerprint: w.deepFingerprint(ctx), Document: draftText(ctx.Store),
		Outcome: outcome, Skip: skip, Rounds: int(count), Runner: runner,
	})
}

// recordDeepSettled records the user's settling as the deep verdict over the
// current draft, which the writing guide's judgment then covers as well.
func (w *WorkflowSession) recordDeepSettled(ctx *engine.Context) error {
	err := w.writeDeepVerdict(ctx, "settled", "")
	if err != nil || !guideRan(ctx.Store) {
		return err
	}
	return writeGuideBase(ctx.Store)
}

// deepSkipCause is the cause of the skip the current deep verdict records,
// empty when none.
func deepSkipCause(store *engine.Store) string {
	verdict, ok := readDeepVerdict(store)
	if !ok || verdict.Outcome != "skipped" {
		return ""
	}
	return verdict.Skip
}

func (w *WorkflowSession) registerWorkflowDeep(registry *engine.Registry) error {
	predicates := []engine.Predicate{
		{
			Doc: engine.FuncDoc{Name: "captureDeepVerdictCurrent", Doc: "The project runs no deep verifier, or a deep verdict or skip covers the current draft, staged attachments and target.", Reads: append(slices.Clone(captureDraftFields), fieldDeepVerdict)},
			Fn:  w.deepVerdictCurrent,
		},
		{
			Doc: engine.FuncDoc{Name: "deepSkipWeighed", Doc: "The project sets verify.skip: weighed, so the user weighs running the deep exchange first."},
			Fn: func(ctx *engine.Context) (bool, error) {
				policy, _, err := w.deepPolicy(ctx.Instance, ctx.Store)
				return policy.Skip == model.VerifierSkipWeighed, err
			},
			Selector: true,
		},
		{
			Doc: engine.FuncDoc{Name: "deepFindingsRefused", Doc: "The verifier's findings still fail the check after its repair: the verifier gave up.", Reads: []string{fieldDeepProblems}},
			Fn: func(ctx *engine.Context) (bool, error) {
				problems, _ := workflowStoreString(ctx.Store, fieldDeepProblems)
				return problems != "", nil
			},
		},
		{
			Doc: engine.FuncDoc{Name: "noDeepFindings", Doc: "The deep verifier raised no finding.", Reads: []string{fieldDeepFindings}},
			Fn: func(ctx *engine.Context) (bool, error) {
				findings, err := storeDeep[model.DeepFinding](ctx.Store, fieldDeepFindings)
				return len(findings) == 0, err
			},
		},
		{
			Doc: engine.FuncDoc{Name: "deepProposalEmpty", Doc: "The proposal changes nothing and no finding waits for the user.", Reads: []string{fieldDeepHunks, fieldDeepOpen}},
			Fn: func(ctx *engine.Context) (bool, error) {
				hunks, _ := workflowStoreString(ctx.Store, fieldDeepHunks)
				open, _ := workflowStoreString(ctx.Store, fieldDeepOpen)
				return hunks == "" && open == "", nil
			},
		},
		{
			Doc: engine.FuncDoc{Name: "deepUserOpen", Doc: "The proposal leaves a point open for the user: a finding only they can settle, or one the drafter contested and the verifier holds.", Reads: []string{fieldDeepOpen}},
			Fn: func(ctx *engine.Context) (bool, error) {
				open, _ := workflowStoreString(ctx.Store, fieldDeepOpen)
				return open != "", nil
			},
		},
		{
			Doc: engine.FuncDoc{Name: "deepDraftChanged", Doc: "The draft differs from the base document the deep exchange judged: an accepted proposal changed it.", Reads: append(slices.Clone(captureDraftFields), fieldDeepBase)},
			Fn: func(ctx *engine.Context) (bool, error) {
				base, _ := workflowStoreString(ctx.Store, fieldDeepBase)
				return draftText(ctx.Store) != base, nil
			},
		},
		{
			Doc: engine.FuncDoc{Name: "deepHeld", Doc: "The deep exchange held its open points for the person over the current draft, staged attachments and target, and no confirm settled them yet.", Reads: append(slices.Clone(captureDraftFields), fieldDeepVerdict)},
			Fn:  w.deepHeld,
		},
		{
			Doc: engine.FuncDoc{Name: "deepEditSettled", Doc: "A guided capture whose draft changed since the deep verdict only by reverting or restoring edits the last exchange's proposal applied, each where it applied it, its attachments and target unchanged.", Reads: append(slices.Clone(captureDraftFields), fieldDeepVerdict, fieldDeepBase, fieldDeepFindings, fieldDeepHistory)},
			Fn:  w.deepEditSettled,
		},
		{
			Doc: engine.FuncDoc{Name: "deepRevising", Doc: "The exchange prepareDeep opened goes to the live verifier as a revision.", Reads: []string{fieldDeepRevising}},
			Fn: func(ctx *engine.Context) (bool, error) {
				revising, _ := ctx.Store.Get(fieldDeepRevising)
				return revising == true, nil
			},
		},
	}
	for _, predicate := range predicates {
		err := registry.RegisterPredicate(predicate)
		if err != nil {
			return err
		}
	}
	commands := []engine.Command{
		{
			Doc: engine.FuncDoc{
				Name: "prepareDeep", Doc: "Opens a deep exchange over the current draft: the base document, the paged reading, the weighing rule's recommendation, and a fresh exchange; for the live verifier, the revision's reading.",
				Reads:  append(slices.Clone(captureDraftFields), "findings", "preflightOverride", "verdicts", fieldDeepVerdict, fieldDeepRevisions, fieldDeepReadIDs, fieldDeepHidden),
				Writes: []string{fieldDeepBase, fieldDeepReading, fieldDeepWeighing, fieldDeepHistory, fieldDeepRounds, fieldDeepProposal, fieldDeepHunks, fieldDeepOpen, fieldDeepWithdrawn, fieldDeepUnedited, fieldDeepReplayed, fieldDeepListed, fieldDeepUserListed, fieldDeepProblems, fieldDeepNoSubagent, fieldDeepForkBase, fieldDeepEndReason, fieldDeepRevising, fieldDeepRevisions, fieldDeepRevisionReading, fieldDeepReadIDs, fieldDeepFresh, fieldDeepHidden},
			},
			Fn: w.prepareDeep,
		},
		{
			Doc: engine.FuncDoc{Name: "sendDeepRevision", Doc: "Sends the live verifier the notice of the revision prepareDeep opened; its pages carry the passages changed since the verdict.", Reads: []string{fieldDeepRevisions}, Writes: []string{fieldDeepRevision}},
			Fn:  w.sendDeepRevision,
		},
		{
			Doc: engine.FuncDoc{Name: "recordNoSubagent", Doc: "Records that the harness has no subagent tool, so the verify delegate falls back to a headless launch.", Writes: []string{fieldDeepNoSubagent}},
			Fn:  func(ctx *engine.Context) error { return ctx.Store.WriteEngine(fieldDeepNoSubagent, true) },
		},
		{
			Doc: engine.FuncDoc{Name: "requireDeepFork", Doc: "Refuses until a verify delegate was dispatched for the exchange prepareDeep opened and did not fail.", Reads: []string{fieldDeepForkBase}},
			Fn: func(ctx *engine.Context) error {
				opened, _ := ctx.Store.Get(fieldDeepForkBase)
				if dispatchedVerifier(w.session, ctx.Instance, deepRoundCount(opened)) {
					return nil
				}
				return fmt.Errorf("no verify delegate was dispatched for this exchange of %s: answering deepDispatch dispatches it, so report failed with the error that answer returned", ctx.Instance)
			},
		},
		{
			Doc: engine.FuncDoc{Name: "recordDeepEnded", Doc: "Records why the verify delegate stopped before it delivered: its round failed or ended with nothing delivered, or it was torn down.", Writes: []string{fieldDeepEndReason}},
			Fn: func(ctx *engine.Context) error {
				reason := "the verify delegate ended"
				for _, fork := range w.session.Forks(ctx.Instance) {
					if fork.Procedure == verifyProcedure && fork.Status == engine.ForkConcluded && fork.Reason != "" {
						reason = fork.Reason
					}
				}
				return ctx.Store.WriteEngine(fieldDeepEndReason, reason)
			},
		},
		{
			Doc:    engine.FuncDoc{Name: "readDeepFindings", Doc: "Lists the verifier's findings for the drafter to answer, apart from those only the user can settle, with which lines marked for the verifier it opened, and the problems still refusing them.", Reads: []string{fieldDeepFindings, fieldDeepBase, fieldDeepReadIDs}, Writes: []string{fieldDeepListed, fieldDeepUserListed, fieldDeepProblems, fieldDeepRounds}},
			Fn:     w.readDeepFindings,
			Rounds: true,
		},
		{
			Doc: engine.FuncDoc{Name: "checkDeepContest", Doc: "Refuses a contest naming no open finding, an owner finding, or none at all, and one once the rounds are spent.", Reads: []string{fieldDeepContest, fieldDeepFindings, fieldDeepRounds}},
			Fn:  w.checkDeepContest,
		},
		{
			Doc:    engine.FuncDoc{Name: "foldDeepResponses", Doc: "Adds the verifier's responses to the exchange and counts the round.", Reads: []string{fieldDeepResponses, fieldDeepHistory, fieldDeepRounds}, Writes: []string{fieldDeepHistory, fieldDeepRounds}},
			Fn:     w.foldDeepResponses,
			Rounds: true,
		},
		{
			Doc: engine.FuncDoc{Name: "buildDeepProposal", Doc: "Builds the proposal from the findings and the latest responses: the changed draft, its hunks labelled by finding, the points open for the user, the findings the verifier withdrew with its reasons, and the findings taken without an edit.", Reads: []string{fieldDeepBase, fieldDeepFindings, fieldDeepHistory, fieldDeepContest}, Writes: []string{fieldDeepProposal, fieldDeepHunks, fieldDeepOpen, fieldDeepWithdrawn, fieldDeepUnedited}},
			Fn:  w.buildDeepProposal,
		},
		{
			Doc: engine.FuncDoc{Name: commandAcceptDeepProposal, Doc: "Records the deep verdict over the draft the accept wrote, and has the writing guide's judgment cover it, so a delta check leaves what the exchange settled alone. sdd merges the proposal into the accept's fields before it lands: the fields the proposal changes take its values, the reported ones stand beside them, and one the proposal changes otherwise is refused.", Reads: append(slices.Clone(captureDraftFields), "guideFindings"), Writes: []string{fieldDeepVerdict, fieldGuideBase}},
			Fn: func(ctx *engine.Context) error {
				err := w.writeDeepVerdict(ctx, "verified", "")
				if err != nil || !guideRan(ctx.Store) {
					return err
				}
				return writeGuideBase(ctx.Store)
			},
		},
		{
			Doc: engine.FuncDoc{Name: "markPlaybackAnswered", Doc: "Marks what a playback carried as shown once it is answered: the deep exchange's settled findings, and the draft the checks began on, so a later round lists only what the checks change after it.", Writes: []string{fieldDeepReplayed, fieldCheckBase}},
			Fn: func(ctx *engine.Context) error {
				if err := ctx.Store.WriteEngine(fieldDeepReplayed, true); err != nil {
					return err
				}
				return ctx.Store.WriteEngine(fieldCheckBase, nil)
			},
		},
		{
			Doc: engine.FuncDoc{Name: "holdDeepOpen", Doc: "Holds the deep exchange's open points for the person as the verdict over the current draft; the person's confirm settles them (recordDeepSettled), and the write refuses a verdict still held.", Reads: captureDraftFields, Writes: []string{fieldDeepVerdict}},
			Fn:  func(ctx *engine.Context) error { return w.writeDeepVerdict(ctx, deepOutcomeHeld, "") },
		},
		{
			Doc: engine.FuncDoc{Name: "recordDeepVerdict", Doc: "Records the deep verdict over the current draft.", Reads: captureDraftFields, Writes: []string{fieldDeepVerdict}},
			Fn:  func(ctx *engine.Context) error { return w.writeDeepVerdict(ctx, "verified", "") },
		},
		{
			Doc: engine.FuncDoc{Name: "recordDeepSettled", Doc: "Records the user's settling of the open points as the deep verdict over the current draft, and has the writing guide's judgment cover it.", Reads: append(slices.Clone(captureDraftFields), "guideFindings"), Writes: []string{fieldDeepVerdict, fieldGuideBase}},
			Fn:  w.recordDeepSettled,
		},
		{
			Doc: engine.FuncDoc{Name: "settleDeepEdit", Doc: "Records the user's revert or restore of the deep exchange's edits as the deep verdict over the current draft, and has the writing guide's judgment cover it.", Reads: append(slices.Clone(captureDraftFields), "guideFindings"), Writes: []string{fieldDeepVerdict, fieldGuideBase}},
			Fn:  w.recordDeepSettled,
		},
		{
			Doc: engine.FuncDoc{Name: "recordDeepSkip", Doc: "Records the user's weighed skip of the deep exchange: the entry carries verification: skipped and confidence at most medium.", Reads: captureDraftFields, Writes: []string{fieldDeepVerdict}},
			Fn:  func(ctx *engine.Context) error { return w.writeDeepVerdict(ctx, "skipped", model.DeepSkipWeighed) },
		},
		{
			Doc: engine.FuncDoc{Name: "recordDeepFailureSkip", Doc: "Records the user's skip after the verifier could not run: the entry carries verification: skipped and confidence low. Refused under verify.skip: never.", Reads: captureDraftFields, Writes: []string{fieldDeepVerdict}},
			Fn: func(ctx *engine.Context) error {
				policy, _, err := w.deepPolicy(ctx.Instance, ctx.Store)
				if err != nil {
					return err
				}
				if policy.Skip == model.VerifierSkipNever {
					return errors.New("verify.skip is never, so the capture waits until the verifier runs: retry once it can")
				}
				return w.writeDeepVerdict(ctx, "skipped", model.DeepSkipFailure)
			},
		},
	}
	for _, command := range commands {
		err := registry.RegisterCommand(command)
		if err != nil {
			return err
		}
	}
	return w.registerVerifyTask(registry)
}

// prepareDeep opens the exchange: the base document every edit is made
// over, the reading paged for the verifier, the weighing rule's
// recommendation, and for a live verifier the revision's reading.
func (w *WorkflowSession) prepareDeep(ctx *engine.Context) error {
	policy, runtime, err := w.deepPolicy(ctx.Instance, ctx.Store)
	if err != nil {
		return err
	}
	revising, fresh, err := w.deepRevisable(ctx)
	if err != nil {
		return err
	}
	base := draftText(ctx.Store)
	writes := map[string]any{
		fieldDeepBase: base, fieldDeepHistory: []model.DeepResponse{}, fieldDeepRounds: 0,
		fieldDeepProposal: nil, fieldDeepHunks: nil, fieldDeepOpen: nil, fieldDeepWithdrawn: nil, fieldDeepUnedited: nil, fieldDeepReplayed: nil, fieldDeepListed: nil, fieldDeepUserListed: nil, fieldDeepProblems: nil, fieldDeepNoSubagent: nil,
		fieldDeepForkBase: verifyForks(w.session, ctx.Instance), fieldDeepEndReason: nil,
		fieldDeepRevising: revising, fieldDeepRevisions: 0, fieldDeepRevisionReading: nil, fieldDeepFresh: nil,
	}
	if revising {
		// The live verifier keeps the launch it started with: one sdd launched
		// for a harness without a subagent tool is never handed to the drafter.
		delete(writes, fieldDeepNoSubagent)
	}
	if fresh != "" {
		writes[fieldDeepFresh] = fresh
	}
	// One placement serves every rendering of the round: another would start
	// a ranking pass over what the first left unscored.
	var placed verifierPlaced
	if ctx.Graph != nil {
		placed, err = w.verifierPlacement(ctx)
		if err != nil {
			return fmt.Errorf("prepareDeep: %w", err)
		}
	}
	reading, held, err := deepWholeReading(ctx, base, placed)
	if err != nil {
		return fmt.Errorf("prepareDeep: %w", err)
	}
	writes[fieldDeepReading] = reading
	writes[fieldDeepReadIDs] = held
	writes[fieldDeepHidden] = uniqueIDs(append(workflowStoreStrings(ctx.Store, fieldDeepHidden), held.Hidden...))
	if revising {
		var previous deepHeld
		if value, ok := ctx.Store.Get(fieldDeepReadIDs); ok && value != nil {
			err := decodeStoreValue(value, &previous)
			if err != nil {
				return fmt.Errorf("prepareDeep: reading %s: %w", fieldDeepReadIDs, err)
			}
		}
		taken, _ := ctx.Store.Get(fieldDeepRevisions)
		number := deepRoundCount(taken) + 1
		writes[fieldDeepRevisions] = number
		revision, err := deepRevisionReading(ctx, base, number, previous, placed)
		if err != nil {
			return fmt.Errorf("prepareDeep: %w", err)
		}
		writes[fieldDeepRevisionReading] = revision
	}
	writes[fieldDeepWeighing] = nil
	if policy.Skip == model.VerifierSkipWeighed {
		writes[fieldDeepWeighing] = weighDeep(ctx.Store, runtime.options.Checkout)
	}
	for _, name := range slices.Sorted(maps.Keys(writes)) {
		err := ctx.Store.WriteEngine(name, writes[name])
		if err != nil {
			return fmt.Errorf("prepareDeep: %w", err)
		}
	}
	return nil
}

// weighDeep is the fixed rule's recommendation, rendered for the user's
// run-or-skip choice.
func weighDeep(store *engine.Store, checkout fs.FS) string {
	kind, _ := workflowStoreString(store, "entryKind")
	layer, _ := workflowStoreString(store, "layer")
	medium := 0
	for _, finding := range workflowStoreDocuments(store, "findings") {
		if finding["severity"] == "medium" {
			medium++
		}
	}
	found := func(path string) bool {
		if checkout == nil {
			return false
		}
		_, err := fs.Stat(checkout, path)
		return err == nil
	}
	weighing := model.WeighDeep(model.DeepDraft{
		Text: draftText(store), Kind: model.Kind(kind), Layer: model.Layer(layer),
		Supersedes: workflowStoreStrings(store, "supersedes"), Closes: workflowStoreStrings(store, "closes"),
		MediumFindings: medium,
	}, found)
	choice := "skip"
	if weighing.Run {
		choice = "run"
	}
	return fmt.Sprintf("Recommended: %s, because %s.", choice, strings.Join(weighing.Reasons, "; "))
}

// deepLookup resolves what deep findings cite: entries in the instance's
// graph, files in its project's checkout, at a revision too, and commits in
// its repository.
func (w *WorkflowSession) deepLookup(ctx *engine.Context) (model.DeepLookup, error) {
	runtime, err := w.targetRuntime(w.instanceProject(ctx.Instance), AccessRead)
	if err != nil {
		return model.DeepLookup{}, err
	}
	lookup := model.DeepLookup{Entry: func(id string) (string, bool) {
		if ctx.Graph == nil || ctx.Graph.ByID[id] == nil {
			return "", false
		}
		e := ctx.Graph.ByID[id]
		return e.Summary + "\n\n" + e.Content, true
	}}
	if checkout := runtime.options.Checkout; checkout != nil {
		lookup.File = func(path string) (string, bool, error) { return checkoutFile(checkout, path) }
	}
	if commits := runtime.options.Commits; commits != nil {
		lookup.Commit = func(hash string) (string, bool, error) { return commitMessage(w.ctx, commits, hash) }
	}
	lookup.FileAt = gitFileAt(w.ctx, runtime.options.Git)
	return lookup, nil
}

// commitMessages is a CommitLookup that also reads a commit's message, the
// text a quote citing the commit must be in.
type commitMessages interface {
	CommitMessage(ctx context.Context, hash string) (message string, found bool, err error)
}

// commitMessage returns the message of the commit hash names. A lookup that
// reads no messages yields an empty one, so a quote citing the commit fails
// its check rather than passing unread.
func commitMessage(ctx context.Context, commits CommitLookup, hash string) (string, bool, error) {
	if messages, ok := commits.(commitMessages); ok {
		return messages.CommitMessage(ctx, hash)
	}
	found, err := commits.HasCommit(ctx, hash)
	return "", found, err
}

func checkoutFile(checkout fs.FS, path string) (string, bool, error) {
	info, err := fs.Stat(checkout, path)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("reading cited file %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", false, nil
	}
	data, err := fs.ReadFile(checkout, path)
	if err != nil {
		return "", false, fmt.Errorf("reading cited file %s: %w", path, err)
	}
	return string(data), true, nil
}

// deepWholeReading renders the verifier's whole reading, led by the draft
// document, its refs' fit, what pre-flight raised and the changed passages
// since the last verdict, and names the entries it serves.
func deepWholeReading(ctx *engine.Context, base string, placed verifierPlaced) (model.DeepReading, deepHeld, error) {
	refFit, err := deepRefFit(ctx)
	if err != nil {
		return model.DeepReading{}, deepHeld{}, err
	}
	var lead strings.Builder
	fmt.Fprintf(&lead, "## Draft\n\n```\n%s\n```\n", base)
	lead.WriteString(refFit)
	lead.WriteString(preflightRaised(ctx.Store))
	if previous, ok := readDeepVerdict(ctx.Store); ok {
		if changed := textdiff.ChangedPassages(previous.Document, base); len(changed) > 0 {
			lead.WriteString("\n## Changed since the last verdict\n\nAn earlier exchange verified the rest of the draft. Judge these passages; read the rest as context only.\n\n")
			for _, passage := range changed {
				lead.WriteString("```\n" + passage + "\n```\n")
			}
		}
	}
	reading, served := deepReading(ctx, lead.String(), preflightRules(ctx.Store), deepHeld{}, placed)
	return reading, served, nil
}

// deepHeld names the entries a verifier's reading served in full and those it
// listed by their brief.
type deepHeld struct {
	Full   []string `json:"full,omitempty"`
	Listed []string `json:"listed,omitempty"`
	// Hidden are the entries the reading set aside and only counted.
	Hidden []string `json:"hidden,omitempty"`
	// Marked are the lines the reading marked for the verifier.
	Marked []string `json:"marked,omitempty"`
	// Read are the entries the drafter had read in full by the reading.
	Read []string `json:"read,omitempty"`
}

// deepRoleTarget is the role line of a declared target in the verifier's
// reading.
const deepRoleTarget model.LightRole = "declared target"

// deepRefFit renders each ref of the draft against the applicability matrix,
// as pre-flight reads it, for the verifier's ref-fit axis.
func deepRefFit(ctx *engine.Context) (string, error) {
	var fits []model.RefFit
	for _, ref := range workflowStoreDocuments(ctx.Store, "refs") {
		id, _ := ref["id"].(string)
		kind, _ := ref["kind"].(string)
		fits = append(fits, model.FitRef(ctx.Graph, model.Ref{ID: id, Kind: model.RefKind(kind)}))
	}
	return llmops.DeepRefFit(fits)
}

// preflightRaised renders what pre-flight raised, for the verifier to leave
// alone.
func preflightRaised(store *engine.Store) string {
	var raised []string
	for _, finding := range workflowStoreDocuments(store, "findings") {
		raised = append(raised, fmt.Sprintf("- [%v] %v: %v", finding["severity"], finding["category"], finding["observation"]))
	}
	if len(raised) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Already raised by pre-flight\n\nAlready raised: do not raise them again.")
	if override, _ := store.Get("preflightOverride"); override != nil {
		b.WriteString(" User overrode the high ones: they are settled.")
	}
	b.WriteString("\n\n" + strings.Join(raised, "\n") + "\n")
	return b.String()
}

// deepReading joins lead and rules with the declared targets in full, then
// the lines of the shared placement in one closing block, an entry the
// drafter dismissed among them with its verdict's code; it leaves out what
// skip holds already, and names what it serves.
func deepReading(ctx *engine.Context, lead string, rules model.DeepContinued, skip deepHeld, placed verifierPlaced) (model.DeepReading, deepHeld) {
	graph := ctx.Graph
	if graph == nil {
		return model.DeepReading{Lead: lead, Rules: rules}, deepHeld{}
	}
	heldFull := map[string]bool{}
	for _, id := range skip.Full {
		heldFull[id] = true
	}
	heldAny := maps.Clone(heldFull)
	for _, id := range skip.Listed {
		heldAny[id] = true
	}
	var served deepHeld
	codes := map[string]string{}
	for _, verdict := range storeVerdicts(ctx.Store) {
		for _, id := range verdict.IDs {
			codes[id] = verdict.Code
		}
	}
	reading := instanceReading(ctx)
	shown, read := map[string]bool{}, map[string]bool{}
	for _, id := range reading.shown {
		shown[id] = true
	}
	for _, id := range reading.full {
		read[id] = true
	}
	seen := map[string]bool{}
	var first, rest []model.DeepReadingBlock
	block := func(id string, role model.LightRole) (model.DeepReadingBlock, bool) {
		e, ok := graph.ByID[id]
		if !ok || seen[id] {
			return model.DeepReadingBlock{}, false
		}
		seen[id] = true
		if heldFull[id] {
			return model.DeepReadingBlock{}, false
		}
		served.Full = append(served.Full, id)
		if _, aside := codes[id]; aside {
			role = model.LightRoleSetAside
		} else if shown[id] && role != deepRoleTarget {
			role = model.LightRoleImplicit
		}
		return llmops.DeepReadingBlock(graph, e, role, codes[id]), true
	}
	placement := placed.placement
	lead += llmops.DeepRankingNote(model.VerifierRankingSources(placement, placed.input), placed.judge, placed.input.Widened)
	for _, id := range placement.Targets {
		if served, ok := block(id, deepRoleTarget); ok {
			first = append(first, served)
		}
	}
	// A revision serves in full what the drafter read in full since the
	// verifier's last reading; the start serves the targets alone.
	served.Read = slices.Clone(reading.full)
	if len(skip.Full)+len(skip.Listed) > 0 {
		for _, id := range reading.full {
			if slices.Contains(skip.Read, id) {
				continue
			}
			if served, ok := block(id, model.LightRoleFull); ok {
				first = append(first, served)
			}
		}
	}
	lines := placement.Lines
	unheld := func(lines []model.Line) []model.Line {
		return slices.DeleteFunc(slices.Clone(lines), func(line model.Line) bool { return heldAny[line.Entry.ID] || seen[line.Entry.ID] })
	}
	lines.Bearing, lines.Doubt, lines.Titles = unheld(lines.Bearing), unheld(lines.Doubt), unheld(lines.Titles)
	lines.Counted = slices.DeleteFunc(slices.Clone(lines.Counted), func(id string) bool { return heldAny[id] || seen[id] })
	for _, group := range lines.SetAside {
		served.Hidden = append(served.Hidden, group.IDs...)
	}
	marked := map[string]bool{}
	for _, line := range slices.Concat(lines.Bearing, lines.Doubt, lines.Titles) {
		served.Listed = append(served.Listed, line.Entry.ID)
		if line.Marked {
			marked[line.Entry.ID] = true
			served.Marked = append(served.Marked, line.Entry.ID)
		}
	}
	drafter := func(id string) string {
		switch {
		case codes[id] != "":
			return "drafter: dismissed, " + codes[id]
		case read[id]:
			return "drafter: read"
		case shown[id]:
			return "drafter: shown, left unanswered"
		case marked[id]:
			return "not read by the drafter"
		}
		return ""
	}
	if len(lines.Bearing)+len(lines.Doubt)+len(lines.Titles)+len(lines.Counted)+len(lines.SetAside) > 0 {
		rest = append(rest, llmops.DeepLinesBlock(graph, lines, drafter))
	}
	return model.DeepReading{Lead: lead, Rules: rules, First: first, Rest: rest}, served
}

// markedOpened states, for each line the verifier's reading marked, whether
// a verify delegate of the capture opened it, as its deliveries reported, and
// logs the statement; empty when the reading marked none.
func (w *WorkflowSession) markedOpened(ctx *engine.Context) (string, error) {
	var held deepHeld
	if value, ok := ctx.Store.Get(fieldDeepReadIDs); ok && value != nil {
		err := decodeStoreValue(value, &held)
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", fieldDeepReadIDs, err)
		}
	}
	if len(held.Marked) == 0 {
		return "", nil
	}
	var reads []string
	for _, fork := range w.session.Forks(ctx.Instance) {
		if fork.Procedure == verifyProcedure {
			reads = append(reads, fork.Reads...)
		}
	}
	var opened, unopened []string
	for _, id := range held.Marked {
		if slices.Contains(reads, id) {
			opened = append(opened, id)
		} else {
			unopened = append(unopened, id)
		}
	}
	slogutils.FromContext(w.ctx).Info("the verifier's marked lines", "instance", ctx.Instance, "opened", opened, "unopened", unopened)
	statement := fmt.Sprintf("The verifier opened %d of the %d lines sdd marked for it", len(opened), len(held.Marked))
	if len(opened) > 0 {
		statement += ": " + strings.Join(opened, ", ")
	}
	if len(unopened) > 0 {
		statement += "; not opened: " + strings.Join(unopened, ", ")
	}
	return statement + ".", nil
}

// preflightRules is the rubric pre-flight's model would have judged the draft
// by, when the verifier judges in its place.
func preflightRules(store *engine.Store) model.DeepContinued {
	rubric, _ := workflowStoreString(store, fieldPreflightRubric)
	if rubric == "" {
		return model.DeepContinued{}
	}
	return model.DeepContinued{
		Intro:     "## Pre-flight's rules\n\nPre-flight's model did not judge this draft: you judge in its place. Apply the rules below as part of your verdict. Raise what they would flag as findings in your own report. Their answer format and the reading they mention do not apply: your reading is the rest of these pages.\n\n",
		Continued: "## Pre-flight's rules, continued\n\n",
		Text:      rubric,
	}
}

func (w *WorkflowSession) readDeepFindings(ctx *engine.Context) error {
	findings, err := storeDeep[model.DeepFinding](ctx.Store, fieldDeepFindings)
	if err != nil {
		return err
	}
	base, _ := workflowStoreString(ctx.Store, fieldDeepBase)
	lookup, err := w.deepLookup(ctx)
	if err != nil {
		return err
	}
	problems, err := model.CheckDeepFindings(base, findings, lookup)
	if err != nil {
		return err
	}
	var answerable, owned strings.Builder
	for i, finding := range findings {
		listed := &answerable
		if finding.Owner {
			listed = &owned
		}
		fmt.Fprintf(listed, "- %s [%s, %s]: %s", model.DeepFindingID(i), finding.Severity, finding.Axis, finding.Verdict)
		if len(finding.Basis) > 0 {
			fmt.Fprintf(listed, " (rests on %s)", strings.Join(finding.Basis, ", "))
		}
		for _, pointer := range finding.Pointers {
			switch {
			case pointer.Quote != "":
				fmt.Fprintf(listed, "\n  - %s: %q", pointer.Field, pointer.Quote)
			case pointer.ID != "":
				fmt.Fprintf(listed, "\n  - %s: %s", pointer.Field, pointer.ID)
			}
		}
		for _, source := range finding.Sources {
			fmt.Fprintf(listed, "\n  - %s %s", source.Kind, source.Ref)
			if source.Quote != "" {
				fmt.Fprintf(listed, ": %q", source.Quote)
			}
		}
		listed.WriteString("\n")
	}
	opened, err := w.markedOpened(ctx)
	if err != nil {
		return err
	}
	if opened != "" && answerable.Len() > 0 {
		answerable.WriteString("\n" + opened + "\n")
	}
	err = ctx.Store.WriteEngine(fieldDeepListed, answerable.String())
	if err != nil {
		return err
	}
	err = ctx.Store.WriteEngine(fieldDeepUserListed, owned.String())
	if err != nil {
		return err
	}
	err = ctx.Store.WriteEngine(fieldDeepRounds, 1)
	if err != nil {
		return err
	}
	err = ctx.Store.WriteEngine(fieldDeepProblems, strings.Join(problems, "\n"))
	if err != nil {
		return err
	}
	return ctx.Round(nil)
}

func (w *WorkflowSession) checkDeepContest(ctx *engine.Context) error {
	contest, err := storeDeep[model.DeepAnswer](ctx.Store, fieldDeepContest)
	if err != nil {
		return err
	}
	findings, err := storeDeep[model.DeepFinding](ctx.Store, fieldDeepFindings)
	if err != nil {
		return err
	}
	rounds, _ := ctx.Store.Get(fieldDeepRounds)
	if deepRoundCount(rounds) >= model.DeepRounds {
		return fmt.Errorf("the verifier has made its %d deliveries: take escalate, so the user settles the open points", model.DeepRounds)
	}
	contested := 0
	seen := map[string]bool{}
	for _, answer := range contest {
		index := -1
		for i := range findings {
			if model.DeepFindingID(i) == answer.Target {
				index = i
			}
		}
		switch {
		case index < 0:
			return fmt.Errorf("%s names no finding: the verifier raised %d", answer.Target, len(findings))
		case findings[index].Owner:
			return fmt.Errorf("%s is the user's to settle, not the verifier's: leave it out of the contest", answer.Target)
		case seen[answer.Target]:
			return fmt.Errorf("%s is answered twice", answer.Target)
		}
		seen[answer.Target] = true
		if answer.Verdict == "contest" {
			contested++
		}
	}
	if contested == 0 {
		return errors.New("a contest names at least one finding with verdict contest and its reason; take accept to accept them all")
	}
	return nil
}

func deepRoundCount(value any) int {
	switch n := value.(type) {
	case int:
		return n
	case float64:
		return int(n)
	}
	return 0
}

func (w *WorkflowSession) foldDeepResponses(ctx *engine.Context) error {
	history, err := storeDeep[model.DeepResponse](ctx.Store, fieldDeepHistory)
	if err != nil {
		return err
	}
	responses, err := storeDeep[model.DeepResponse](ctx.Store, fieldDeepResponses)
	if err != nil {
		return err
	}
	err = ctx.Store.WriteEngine(fieldDeepHistory, append(history, responses...))
	if err != nil {
		return err
	}
	rounds, _ := ctx.Store.Get(fieldDeepRounds)
	round := deepRoundCount(rounds) + 1
	err = ctx.Store.WriteEngine(fieldDeepRounds, round)
	if err != nil {
		return err
	}
	return ctx.Round(nil)
}

func (w *WorkflowSession) buildDeepProposal(ctx *engine.Context) error {
	base, _ := workflowStoreString(ctx.Store, fieldDeepBase)
	findings, err := storeDeep[model.DeepFinding](ctx.Store, fieldDeepFindings)
	if err != nil {
		return err
	}
	history, err := storeDeep[model.DeepResponse](ctx.Store, fieldDeepHistory)
	if err != nil {
		return err
	}
	contest, err := storeDeep[model.DeepAnswer](ctx.Store, fieldDeepContest)
	if err != nil {
		return err
	}
	changes := model.DeepChanges(findings, history)
	proposal, hunks, err := model.BuildDeepProposal(base, changes)
	if err != nil {
		return fmt.Errorf("buildDeepProposal: the verifier's edits no longer apply: %w", err)
	}
	edited := map[string][]model.DeepEdit{}
	for _, change := range changes {
		edited[change.Finding] = change.Edits
	}
	var open, withdrawn, unedited strings.Builder
	for i, finding := range findings {
		id := model.DeepFindingID(i)
		label := model.DeepFindingLabel(id, finding)
		if finding.Owner {
			fmt.Fprintf(&open, "- %s: only the user can settle it\n", label)
			continue
		}
		var response *model.DeepResponse
		for j := len(history) - 1; j >= 0; j-- {
			if history[j].Target == id {
				response = &history[j]
				break
			}
		}
		if response != nil && (response.Outcome == model.DeepConceded || response.Outcome == model.DeepDropped) {
			withdrawn.WriteString(model.DeepSettledLine(id, finding, "withdrawn", response.Note))
			continue
		}
		var answer *model.DeepAnswer
		for j := range contest {
			if contest[j].Target == id && contest[j].Verdict == "contest" {
				answer = &contest[j]
			}
		}
		if answer != nil && response != nil && response.Outcome == model.DeepHeld {
			fmt.Fprintf(&open, "- %s: contested, and the verifier holds it\n  the drafter: %s\n  the verifier: %s\n", label, answer.Reason, response.Note)
			continue
		}
		if edited[id] == nil {
			unedited.WriteString(model.DeepSettledLine(id, finding, "accepted without an edit", ""))
		}
	}
	writes := map[string]any{fieldDeepProposal: proposal, fieldDeepHunks: model.RenderDeepHunks(hunks), fieldDeepOpen: open.String(), fieldDeepWithdrawn: withdrawn.String(), fieldDeepUnedited: unedited.String()}
	for _, name := range slices.Sorted(maps.Keys(writes)) {
		err := ctx.Store.WriteEngine(name, writes[name])
		if err != nil {
			return err
		}
	}
	return nil
}

// Engine-written fields of the verify task.
const (
	fieldVerifyBase    = "verifyBase"
	fieldVerifyReading = "verifyReading"
	fieldVerifyPageAt  = "verifyPageAt"
	// fieldVerifyPageBound is the result bound the reading was last turned
	// under; fieldVerifyRestarted marks a turn that started the reading
	// again because the bound changed.
	fieldVerifyPageBound = "verifyPageBound"
	fieldVerifyRestarted = "verifyRestarted"
	fieldVerifyProblems  = "problems"
	fieldVerifyRepair    = "repairSpent"
	fieldVerifyResponded = "responseHistory"
)

// dispatchingStore is the store of the capture a verify delegate works for:
// held in memory when the fork's own call runs, replayed otherwise.
func (w *WorkflowSession) dispatchingStore() (*engine.Store, error) {
	lineage := w.session.Lineage
	if lineage == nil {
		return nil, errors.New("the verify task runs only as the delegate of a capture")
	}
	parent := w.held[SessionID(lineage.Session)]
	if parent == nil {
		linked, refusal, err := w.app.loadLinked(w.ctx, w.identity, SessionID(lineage.Session))
		if err != nil {
			return nil, err
		}
		if refusal != "" {
			return nil, errors.New(refusal)
		}
		parent = linked
	}
	inst, ok := parent.session.Instance(lineage.Instance)
	if !ok {
		return nil, fmt.Errorf("the dispatching move %s is gone", lineage.Instance)
	}
	return inst.Store, nil
}

// openVerifyReading writes writes, the whole reading among them, into the
// verify task and stands it at page 1.
func openVerifyReading(store *engine.Store, writes map[string]any) error {
	writes[fieldVerifyPageAt], writes[fieldVerifyPageBound], writes[fieldVerifyRestarted] = 0, nil, nil
	for _, name := range slices.Sorted(maps.Keys(writes)) {
		err := store.WriteEngine(name, writes[name])
		if err != nil {
			return err
		}
	}
	return nil
}

// verifyPages pages the verify task's reading for the host the current call
// serves, and returns the page the task stands at.
func (w *WorkflowSession) verifyPages(store *engine.Store) ([]model.DeepReadingPage, int, error) {
	value, ok := store.Get(fieldVerifyReading)
	if !ok || value == nil {
		return nil, 0, errors.New("the verify task holds no reading")
	}
	var reading model.DeepReading
	err := decodeStoreValue(value, &reading)
	if err != nil {
		return nil, 0, fmt.Errorf("reading %s: %w", fieldVerifyReading, err)
	}
	pages := reading.Pages(resultBound(w.ctx))
	at, _ := store.Get(fieldVerifyPageAt)
	return pages, min(deepRoundCount(at), len(pages)-1), nil
}

// verifyBoundChanged reports whether the current call's host takes pages of
// another size than the one the reading was last turned under, so the page
// the task stands at no longer follows the pages served before it.
func (w *WorkflowSession) verifyBoundChanged(store *engine.Store) bool {
	turned, _ := store.Get(fieldVerifyPageBound)
	return turned != nil && deepRoundCount(turned) != int(resultBound(w.ctx))
}

// turnVerifyPage stands the reading at page at, turned under the current
// call's bound.
func (w *WorkflowSession) turnVerifyPage(store *engine.Store, at int, restarted bool) error {
	writes := map[string]any{fieldVerifyPageAt: at, fieldVerifyPageBound: int(resultBound(w.ctx)), fieldVerifyRestarted: nil}
	if restarted {
		writes[fieldVerifyRestarted] = true
	}
	for _, name := range slices.Sorted(maps.Keys(writes)) {
		err := store.WriteEngine(name, writes[name])
		if err != nil {
			return err
		}
	}
	return nil
}

func verifyPageNote(store *engine.Store, at, count int) string {
	note := fmt.Sprintf("Page %d of %d.", at+1, count)
	if restarted, _ := store.Get(fieldVerifyRestarted); restarted == true {
		note = "This host takes pages of another size than the pages served before, so the reading starts again. " + note
	}
	if at < count-1 {
		return note + " Report more for the next page. Findings are refused until every page is served."
	}
	return note + " Every page is served."
}

// verifyPageFullIDs returns the entries the current page of the reading serves
// in full.
func (w *WorkflowSession) verifyPageFullIDs(store *engine.Store) ([]string, error) {
	pages, at, err := w.verifyPages(store)
	if err != nil {
		return nil, err
	}
	return pages[at].FullIDs, nil
}

func (w *WorkflowSession) registerVerifyTask(registry *engine.Registry) error {
	// The deep exchange's documents are the exchange itself: a page, the
	// findings, the proposal and its hunks reach their reader whole, never at
	// the store-value cap. Both queries cut for the host the call serves; the
	// registered cap is the default host's, which the authoring arithmetic
	// sizes against.
	pageCap := engine.QueryBound{Part: serveview.PartText, Cap: serveview.Cap{MaxBytes: model.DefaultResultBound.DeepPage()}}
	err := registry.RegisterQuery(engine.Query{
		Doc:       engine.FuncDoc{Name: "deepDocument", Doc: "Serves one deep-exchange store value whole: the findings, the proposal or its hunks, up to one page of the reading. Arg unplayed: nothing once a playback carrying the settled findings was answered (markPlaybackAnswered)."},
		ServeSafe: true,
		Bound:     pageCap,
		Fn: func(ctx *engine.Context, args map[string]any) (any, error) {
			if unplayed, _ := args["unplayed"].(bool); unplayed {
				if replayed, _ := ctx.Store.Get(fieldDeepReplayed); replayed == true {
					return "", nil
				}
			}
			field, _ := args["field"].(string)
			value, _ := workflowStoreString(ctx.Store, field)
			return truncate.Bytes(value, resultBound(w.ctx).DeepPage(), ""), nil
		},
	})
	if err != nil {
		return err
	}
	err = registry.RegisterQuery(engine.Query{
		Doc:       engine.FuncDoc{Name: "verifyPage", Doc: "Serves the page of the reading the verify task stands at, cut for the host of the call that serves it, or with part: note the line naming the page.", Reads: []string{fieldVerifyReading, fieldVerifyPageAt, fieldVerifyRestarted}},
		ServeSafe: true,
		Bound:     pageCap,
		Fn: func(ctx *engine.Context, args map[string]any) (any, error) {
			pages, at, err := w.verifyPages(ctx.Store)
			if err != nil {
				return nil, err
			}
			text := pages[at].Text
			if part, _ := args["part"].(string); part == "note" {
				text = verifyPageNote(ctx.Store, at, len(pages))
			}
			return truncate.Text{Text: text, Cut: truncate.Cut{KeptBytes: len(text), TotalBytes: len(text)}}, nil
		},
	})
	if err != nil {
		return err
	}
	predicates := []engine.Predicate{
		{
			Doc: engine.FuncDoc{Name: "verifyFindingsStand", Doc: "The verifier's findings pass the check: valid, resolving, and with edits that apply to the base draft.", Reads: []string{fieldVerifyProblems}},
			Fn: func(ctx *engine.Context) (bool, error) {
				problems, _ := workflowStoreString(ctx.Store, fieldVerifyProblems)
				return problems == "", nil
			},
		},
		{
			Doc: engine.FuncDoc{Name: "verifyRepairSpent", Doc: "The verifier's one repair of refused findings is spent.", Reads: []string{fieldVerifyRepair}},
			Fn: func(ctx *engine.Context) (bool, error) {
				spent, _ := ctx.Store.Get(fieldVerifyRepair)
				return spent == true, nil
			},
		},
		{
			Doc: engine.FuncDoc{Name: "verifyRevised", Doc: "The capture sent a revision of the draft."},
			Fn: func(ctx *engine.Context) (bool, error) {
				return slices.Contains(ctx.Arrived, "revision"), nil
			},
		},
	}
	for _, predicate := range predicates {
		err := registry.RegisterPredicate(predicate)
		if err != nil {
			return err
		}
	}
	commands := []engine.Command{
		{
			Doc: engine.FuncDoc{Name: "loadVerifyRevision", Doc: "Loads the revised draft and the revision's whole reading from the capture this delegate works for, stands it at page 1, and opens a fresh exchange over the revised draft.", Writes: []string{fieldVerifyBase, fieldVerifyReading, fieldVerifyPageAt, fieldVerifyPageBound, fieldVerifyRestarted, fieldVerifyRepair, fieldVerifyResponded}},
			Fn:  w.loadVerifyRevision,
		},
		{
			Doc: engine.FuncDoc{Name: "loadVerifyInput", Doc: "Loads the draft document and the whole reading from the capture this delegate works for, and stands it at page 1: on start, and for a verifier that does not hold the reading.", Writes: []string{fieldVerifyBase, fieldVerifyReading, fieldVerifyPageAt, fieldVerifyPageBound, fieldVerifyRestarted}},
			Fn: func(ctx *engine.Context) error {
				parent, err := w.dispatchingStore()
				if err != nil {
					return err
				}
				base, _ := workflowStoreString(parent, fieldDeepBase)
				reading, _ := parent.Get(fieldDeepReading)
				if base == "" || reading == nil {
					return errors.New("the dispatching capture holds no prepared deep exchange")
				}
				return openVerifyReading(ctx.Store, map[string]any{fieldVerifyBase: base, fieldVerifyReading: reading})
			},
		},
		{
			Doc: engine.FuncDoc{Name: "turnPage", Doc: "Serves the next page of the reading, or page 1 again when the call's host takes pages of another size than the reading was turned under.", Reads: []string{fieldVerifyReading, fieldVerifyPageAt, fieldVerifyPageBound}, Writes: []string{fieldVerifyPageAt, fieldVerifyPageBound, fieldVerifyRestarted}},
			Fn: func(ctx *engine.Context) error {
				if w.verifyBoundChanged(ctx.Store) {
					return w.turnVerifyPage(ctx.Store, 0, true)
				}
				pages, at, err := w.verifyPages(ctx.Store)
				if err != nil {
					return err
				}
				if at+1 >= len(pages) {
					return errors.New("every page is served: report judge with your findings")
				}
				return w.turnVerifyPage(ctx.Store, at+1, false)
			},
		},
		{
			Doc: engine.FuncDoc{Name: "requireReadingServed", Doc: "Refuses findings until every page of the reading was served at the call's page size.", Reads: []string{fieldVerifyReading, fieldVerifyPageAt, fieldVerifyPageBound}},
			Fn: func(ctx *engine.Context) error {
				if w.verifyBoundChanged(ctx.Store) {
					return errors.New("this host takes pages of another size than the pages served before: report more to read the reading again from page 1, then judge")
				}
				pages, at, err := w.verifyPages(ctx.Store)
				if err != nil {
					return err
				}
				if at+1 < len(pages) {
					return fmt.Errorf("page %d of %d is the last served: report more until every page is served, then judge", at+1, len(pages))
				}
				return requireFindingsReported(ctx.Store)
			},
		},
		{
			Doc: engine.FuncDoc{Name: "checkVerifyFindings", Doc: "Checks the findings: valid, resolving, and with edits that apply to the base draft exactly once and without overlap.", Reads: []string{"findings", fieldVerifyBase}, Writes: []string{fieldVerifyProblems}},
			Fn: func(ctx *engine.Context) error {
				findings, err := storeDeep[model.DeepFinding](ctx.Store, "findings")
				if err != nil {
					return err
				}
				base, _ := workflowStoreString(ctx.Store, fieldVerifyBase)
				lookup, err := w.deepLookup(ctx)
				if err != nil {
					return err
				}
				problems, err := model.CheckDeepFindings(base, findings, lookup)
				if err != nil {
					return err
				}
				return ctx.Store.WriteEngine(fieldVerifyProblems, strings.Join(problems, "\n"))
			},
		},
		{
			Doc: engine.FuncDoc{Name: "spendRepair", Doc: "Spends the verifier's one repair of refused findings.", Writes: []string{fieldVerifyRepair}},
			Fn: func(ctx *engine.Context) error {
				err := requireFindingsReported(ctx.Store)
				if err != nil {
					return err
				}
				return ctx.Store.WriteEngine(fieldVerifyRepair, true)
			},
		},
		{
			Doc: engine.FuncDoc{Name: "checkVerifyResponses", Doc: "Refuses responses that leave a contested finding unanswered, answer an uncontested one, or carry edits that do not apply.", Reads: []string{"responses", "answers", "findings", fieldVerifyBase, fieldVerifyResponded}, Writes: []string{fieldVerifyResponded}},
			Fn:  w.checkVerifyResponses,
		},
	}
	for _, command := range commands {
		err := registry.RegisterCommand(command)
		if err != nil {
			return err
		}
	}
	return nil
}

func (w *WorkflowSession) checkVerifyResponses(ctx *engine.Context) error {
	responses, err := storeDeep[model.DeepResponse](ctx.Store, "responses")
	if err != nil {
		return err
	}
	answers, err := storeDeep[model.DeepAnswer](ctx.Store, "answers")
	if err != nil {
		return err
	}
	findings, err := storeDeep[model.DeepFinding](ctx.Store, "findings")
	if err != nil {
		return err
	}
	history, err := storeDeep[model.DeepResponse](ctx.Store, fieldVerifyResponded)
	if err != nil {
		return err
	}
	contested := map[string]bool{}
	for _, answer := range answers {
		if answer.Verdict == "contest" {
			contested[answer.Target] = true
		}
	}
	answered := map[string]bool{}
	for _, response := range responses {
		if !contested[response.Target] {
			return fmt.Errorf("%s is not contested: respond to the contested findings only", response.Target)
		}
		answered[response.Target] = true
	}
	for _, target := range slices.Sorted(maps.Keys(contested)) {
		if !answered[target] {
			return fmt.Errorf("%s is contested and has no response", target)
		}
	}
	base, _ := workflowStoreString(ctx.Store, fieldVerifyBase)
	all := append(slices.Clone(history), responses...)
	_, _, err = model.BuildDeepProposal(base, model.DeepChanges(findings, all))
	if err != nil {
		return fmt.Errorf("the edits do not apply to the base draft: %w", err)
	}
	return ctx.Store.WriteEngine(fieldVerifyResponded, all)
}

// verifyForks counts the verify delegates instance dispatched, live or not.
// dispatchedVerifier reports whether one of the instance's verify forks after
// the first opened ones is live or completed: a fork that concluded, its
// launch or round failed, dispatched nothing.
func dispatchedVerifier(session *engine.Session, instance string, opened int) bool {
	seen := 0
	for _, fork := range session.Forks(instance) {
		if fork.Procedure != verifyProcedure {
			continue
		}
		seen++
		if seen > opened && fork.Status != engine.ForkConcluded {
			return true
		}
	}
	return false
}

func verifyForks(session *engine.Session, instance string) int {
	count := 0
	for _, fork := range session.Forks(instance) {
		if fork.Procedure == verifyProcedure {
			count++
		}
	}
	return count
}

// requireFindingsReported refuses a verdict that reports no findings list:
// an empty list is the verdict that nothing is wrong.
func requireFindingsReported(store *engine.Store) error {
	if _, ok := store.Get("findings"); !ok {
		return errors.New("report findings: the whole list, an empty one when nothing is wrong")
	}
	return nil
}
