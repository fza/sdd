package application

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/serveview"
	"github.com/networkteam/sdd/internal/textdiff"
)

// The deep draft verifier (20261109-004729-d-cpt-xs5, slice 3): once the
// light pre-flight leaves no high finding open, the capture dispatches the
// verify task into a child session, exchanges findings, answers and responses
// with it for at most model.DeepRounds deliveries, and records a deep verdict
// over the draft it settled; the write requires that verdict to be current.

const verifyProcedure = "verify"

// verifyReadStep is the verify step that serves the current page of the reading.
const verifyReadStep = "read"

// Engine-written fields of the capture's deep exchange.
const (
	fieldDeepVerdict    = "deepVerdict"
	fieldDeepBase       = "deepBase"
	fieldDeepPages      = "deepPages"
	fieldDeepPageFull   = "deepPageFull"
	fieldDeepWeighing   = "deepWeighing"
	fieldDeepHistory    = "deepHistory"
	fieldDeepRounds     = "deepRounds"
	fieldDeepProposal   = "deepProposal"
	fieldDeepHunks      = "deepHunks"
	fieldDeepOpen       = "deepOpen"
	fieldDeepListed     = "deepListed"
	fieldDeepProblems   = "deepProblems"
	fieldDeepNoSubagent = "deepNoSubagent"
	fieldDeepForkBase   = "deepForkBase"
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
// capture input it covers (V-D26).
type deepVerdict struct {
	Fingerprint string `json:"fingerprint"`
	// Document is the draft document the verdict covers, which a later
	// exchange's delta reading compares against.
	Document string `json:"document"`
	// Outcome is verified, settled (by the user after the rounds) or skipped.
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
	sum := sha256.New()
	sum.Write([]byte(draftText(ctx.Store)))
	for _, handle := range workflowStoreStrings(ctx.Store, "attachments") {
		fmt.Fprintf(sum, "\x00%s=%s", handle, w.staged[handle])
	}
	target, _ := w.effectiveTargetFor(w.instanceProject(ctx.Instance), ctx.Store)
	fmt.Fprintf(sum, "\x00%s\x00%s", target.Project, target.Branch)
	return hex.EncodeToString(sum.Sum(nil))
}

func (w *WorkflowSession) deepPolicy(instance string) (model.VerifierPolicy, *ProjectRuntime, error) {
	runtime, err := w.targetRuntime(w.instanceProject(instance), AccessRead)
	if err != nil {
		return model.VerifierPolicy{}, nil, err
	}
	return runtime.options.Verifier.resolved(), runtime, nil
}

// deepVerdictCurrent reports whether the write may go ahead as far as the
// deep verifier is concerned: the project runs no deep verifier, or a
// verdict or skip covers the current input.
func (w *WorkflowSession) deepVerdictCurrent(ctx *engine.Context) (bool, error) {
	policy, _, err := w.deepPolicy(ctx.Instance)
	if err != nil || policy.Mode != model.VerifierDeep {
		return true, err
	}
	verdict, ok := readDeepVerdict(ctx.Store)
	return ok && verdict.Fingerprint == w.deepFingerprint(ctx), nil
}

// deepJudgesPreflight reports whether the deep verifier judges the draft in
// pre-flight's place, so pre-flight runs only its mechanical checks: the
// project runs deep, and no skip of the deep exchange covers the current
// draft. A skipped exchange falls back to pre-flight's model, so deep never
// checks less than prose.
func (w *WorkflowSession) deepJudgesPreflight(ctx *engine.Context) (bool, error) {
	policy, _, err := w.deepPolicy(ctx.Instance)
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
	policy, _, err := w.deepPolicy(ctx.Instance)
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
			Doc: engine.FuncDoc{Name: "captureDeepVerdictCurrent", Doc: "The project runs no deep verifier, or a deep verdict or skip covers the current draft, staged attachments and target (V-D26).", Reads: append(slices.Clone(captureDraftFields), fieldDeepVerdict)},
			Fn:  w.deepVerdictCurrent,
		},
		{
			Doc: engine.FuncDoc{Name: "deepSkipWeighed", Doc: "The project sets verify.skip: weighed, so the user weighs running the deep exchange first."},
			Fn: func(ctx *engine.Context) (bool, error) {
				policy, _, err := w.deepPolicy(ctx.Instance)
				return policy.Skip == model.VerifierSkipWeighed, err
			},
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
			Doc: engine.FuncDoc{Name: "deepOwnerOpen", Doc: "A finding only the user can settle is open.", Reads: []string{fieldDeepFindings}},
			Fn: func(ctx *engine.Context) (bool, error) {
				findings, err := storeDeep[model.DeepFinding](ctx.Store, fieldDeepFindings)
				return slices.ContainsFunc(findings, func(f model.DeepFinding) bool { return f.Owner }), err
			},
		},
		{
			Doc: engine.FuncDoc{Name: "deepDraftChanged", Doc: "The draft differs from the base document the deep exchange judged: an accepted proposal changed it.", Reads: append(slices.Clone(captureDraftFields), fieldDeepBase)},
			Fn: func(ctx *engine.Context) (bool, error) {
				base, _ := workflowStoreString(ctx.Store, fieldDeepBase)
				return draftText(ctx.Store) != base, nil
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
				Name: "prepareDeep", Doc: "Opens a deep exchange over the current draft: the base document, the paged reading, the weighing rule's recommendation, and a fresh exchange.",
				Reads:  append(slices.Clone(captureDraftFields), "findings", "preflightOverride", "verdicts", fieldDeepVerdict),
				Writes: []string{fieldDeepBase, fieldDeepPages, fieldDeepPageFull, fieldDeepWeighing, fieldDeepHistory, fieldDeepRounds, fieldDeepProposal, fieldDeepHunks, fieldDeepOpen, fieldDeepListed, fieldDeepProblems, fieldDeepNoSubagent, fieldDeepForkBase},
			},
			Fn: w.prepareDeep,
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
			Doc:    engine.FuncDoc{Name: "readDeepFindings", Doc: "Lists the verifier's findings for the drafter to answer, and the problems still refusing them.", Reads: []string{fieldDeepFindings, fieldDeepBase}, Writes: []string{fieldDeepListed, fieldDeepProblems, fieldDeepRounds}},
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
			Doc: engine.FuncDoc{Name: "buildDeepProposal", Doc: "Builds the proposal from the findings and the latest responses: the changed draft, its hunks labelled by finding, and the points open for the user.", Reads: []string{fieldDeepBase, fieldDeepFindings, fieldDeepHistory, fieldDeepContest}, Writes: []string{fieldDeepProposal, fieldDeepHunks, fieldDeepOpen}},
			Fn:  w.buildDeepProposal,
		},
		{
			Doc: engine.FuncDoc{Name: commandAcceptDeepProposal, Doc: "Refuses a draft that differs from the proposal; records the deep verdict over the draft once it matches. An accept reporting no draft field carries the fields the proposal changes, written by sdd.", Reads: append(slices.Clone(captureDraftFields), fieldDeepProposal), Writes: []string{fieldDeepVerdict}},
			Fn: func(ctx *engine.Context) error {
				proposal, _ := workflowStoreString(ctx.Store, fieldDeepProposal)
				current := draftText(ctx.Store)
				if current != proposal {
					return fmt.Errorf("the draft differs from the proposal; report the fields so the draft matches it, or none so sdd writes the proposal:\n%s", textdiff.Unified(proposal, current))
				}
				return w.writeDeepVerdict(ctx, "verified", "")
			},
		},
		{
			Doc: engine.FuncDoc{Name: "recordDeepVerdict", Doc: "Records the deep verdict over the current draft.", Reads: captureDraftFields, Writes: []string{fieldDeepVerdict}},
			Fn:  func(ctx *engine.Context) error { return w.writeDeepVerdict(ctx, "verified", "") },
		},
		{
			Doc: engine.FuncDoc{Name: "recordDeepSettled", Doc: "Records the user's settling of the open points as the deep verdict over the current draft.", Reads: captureDraftFields, Writes: []string{fieldDeepVerdict}},
			Fn:  func(ctx *engine.Context) error { return w.writeDeepVerdict(ctx, "settled", "") },
		},
		{
			Doc: engine.FuncDoc{Name: "recordDeepSkip", Doc: "Records the user's weighed skip of the deep exchange: the entry carries verification: skipped and confidence at most medium.", Reads: captureDraftFields, Writes: []string{fieldDeepVerdict}},
			Fn:  func(ctx *engine.Context) error { return w.writeDeepVerdict(ctx, "skipped", model.DeepSkipWeighed) },
		},
		{
			Doc: engine.FuncDoc{Name: "recordDeepFailureSkip", Doc: "Records the user's skip after the verifier could not run: the entry carries verification: skipped and confidence low. Refused under verify.skip: never.", Reads: captureDraftFields, Writes: []string{fieldDeepVerdict}},
			Fn: func(ctx *engine.Context) error {
				policy, _, err := w.deepPolicy(ctx.Instance)
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
// over, the reading paged for the verifier (V-D8), and the weighing rule's
// recommendation.
func (w *WorkflowSession) prepareDeep(ctx *engine.Context) error {
	policy, runtime, err := w.deepPolicy(ctx.Instance)
	if err != nil {
		return err
	}
	base := draftText(ctx.Store)
	writes := map[string]any{
		fieldDeepBase: base, fieldDeepHistory: []model.DeepResponse{}, fieldDeepRounds: 0,
		fieldDeepProposal: nil, fieldDeepHunks: nil, fieldDeepOpen: nil, fieldDeepListed: nil, fieldDeepProblems: nil, fieldDeepNoSubagent: nil,
		fieldDeepForkBase: verifyForks(w.session, ctx.Instance),
	}
	pages := w.deepPages(ctx, base)
	texts := make([]string, len(pages))
	full := make([][]string, len(pages))
	for i, page := range pages {
		texts[i], full[i] = page.Text, page.FullIDs
	}
	writes[fieldDeepPages], writes[fieldDeepPageFull] = texts, full
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
// run-or-skip choice (V-D24).
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

// deepPages renders the verifier's reading: page 1 the draft document, what
// pre-flight raised, the changed passages since the last verdict, the
// declared targets and the entries read in full; later pages the briefs, in
// the order the reading listed them, each set-aside with its code.
func (w *WorkflowSession) deepPages(ctx *engine.Context, base string) []model.DeepReadingPage {
	var lead strings.Builder
	fmt.Fprintf(&lead, "## Draft\n\n```\n%s\n```\n", base)
	var raised []string
	for _, finding := range workflowStoreDocuments(ctx.Store, "findings") {
		raised = append(raised, fmt.Sprintf("- [%v] %v: %v", finding["severity"], finding["category"], finding["observation"]))
	}
	if len(raised) > 0 {
		lead.WriteString("\n## Already raised by pre-flight\n\nThese are raised already; do not raise them again.")
		if override, _ := ctx.Store.Get("preflightOverride"); override != nil {
			lead.WriteString(" The user overrode the high ones: they are settled.")
		}
		lead.WriteString("\n\n" + strings.Join(raised, "\n") + "\n")
	}
	if previous, ok := readDeepVerdict(ctx.Store); ok {
		if changed := textdiff.ChangedPassages(previous.Document, base); len(changed) > 0 {
			lead.WriteString("\n## Changed since the last verdict\n\nAn earlier exchange verified the rest of the draft: judge these passages, the rest as context only.\n\n")
			for _, passage := range changed {
				lead.WriteString("```\n" + passage + "\n```\n")
			}
		}
	}
	graph := ctx.Graph
	if graph == nil {
		return []model.DeepReadingPage{{Text: lead.String()}}
	}
	codes := map[string]string{}
	for _, verdict := range storeVerdicts(ctx.Store) {
		for _, id := range verdict.IDs {
			codes[id] = verdict.Code
		}
	}
	seen := map[string]bool{}
	var first, rest []model.DeepReadingBlock
	block := func(id string, role model.LightRole, full bool) (model.DeepReadingBlock, bool) {
		e, ok := graph.ByID[id]
		if !ok || seen[id] {
			return model.DeepReadingBlock{}, false
		}
		seen[id] = true
		if _, aside := codes[id]; aside {
			role = model.LightRoleSetAside
		}
		text := model.EntryBrief(e)
		if full {
			text = e.Content
		}
		return model.DeepReadingBlock{ID: e.ID, Text: "---\n\n" + llmops.ReadingHeader(graph, e, role, codes[id]) + "\n" + text}, true
	}
	var targets []string
	for _, ref := range workflowStoreDocuments(ctx.Store, "refs") {
		if id, _ := ref["id"].(string); id != "" {
			targets = append(targets, id)
		}
	}
	targets = append(append(targets, workflowStoreStrings(ctx.Store, "supersedes")...), workflowStoreStrings(ctx.Store, "closes")...)
	for _, id := range targets {
		served, ok := block(id, "declared target", true)
		if ok {
			first = append(first, served)
		}
	}
	reading := instanceReading(ctx)
	for _, id := range reading.full {
		served, ok := block(id, model.LightRoleFull, true)
		if ok {
			first = append(first, served)
		}
	}
	for _, id := range reading.listed {
		served, ok := block(id, model.LightRoleBrief, false)
		if ok {
			rest = append(rest, served)
		}
	}
	return withPreflightRubric(model.DeepReadingPages(lead.String(), first, rest), ctx.Store)
}

// withPreflightRubric serves the rubric pre-flight's model would have judged
// the draft by from page 2, when the verifier judges in its place.
func withPreflightRubric(pages []model.DeepReadingPage, store *engine.Store) []model.DeepReadingPage {
	rubric, _ := workflowStoreString(store, fieldPreflightRubric)
	if rubric == "" {
		return pages
	}
	rules := model.DeepContinuedPages("## Pre-flight's rules\n\nPre-flight's model did not judge this draft: you judge in its place. Apply the rules below as part of your verdict and raise what they would flag as findings in your own report. Their answer format and the reading they mention do not apply: your reading is the rest of these pages.\n\n",
		"## Pre-flight's rules, continued\n\n", rubric)
	return slices.Concat(pages[:1], rules, pages[1:])
}

func (w *WorkflowSession) readDeepFindings(ctx *engine.Context) error {
	findings, err := storeDeep[model.DeepFinding](ctx.Store, fieldDeepFindings)
	if err != nil {
		return err
	}
	base, _ := workflowStoreString(ctx.Store, fieldDeepBase)
	problems := model.CheckDeepFindings(base, findings, func(id string) bool { return ctx.Graph != nil && ctx.Graph.ByID[id] != nil })
	var listed strings.Builder
	for i, finding := range findings {
		owner := ""
		if finding.Owner {
			owner = " (only the user can settle it, so it goes to them)"
		}
		fmt.Fprintf(&listed, "- %s [%s, %s]%s: %s", model.DeepFindingID(i), finding.Severity, finding.Axis, owner, finding.Verdict)
		if len(finding.Basis) > 0 {
			fmt.Fprintf(&listed, " (rests on %s)", strings.Join(finding.Basis, ", "))
		}
		for _, pointer := range finding.Pointers {
			switch {
			case pointer.Quote != "":
				fmt.Fprintf(&listed, "\n  - %s: %q", pointer.Field, pointer.Quote)
			case pointer.ID != "":
				fmt.Fprintf(&listed, "\n  - %s: %s", pointer.Field, pointer.ID)
			}
		}
		if len(finding.Sources) > 0 {
			fmt.Fprintf(&listed, "\n  - sources: %s", strings.Join(finding.Sources, ", "))
		}
		listed.WriteString("\n")
	}
	err = ctx.Store.WriteEngine(fieldDeepListed, listed.String())
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
	proposal, hunks, err := model.BuildDeepProposal(base, model.DeepChanges(findings, history))
	if err != nil {
		return fmt.Errorf("buildDeepProposal: the verifier's edits no longer apply: %w", err)
	}
	var open strings.Builder
	for i, finding := range findings {
		id := model.DeepFindingID(i)
		if finding.Owner {
			fmt.Fprintf(&open, "- %s (only the user can settle it): %s\n", id, finding.Verdict)
			continue
		}
		var response *model.DeepResponse
		for j := len(history) - 1; j >= 0; j-- {
			if history[j].Target == id {
				response = &history[j]
				break
			}
		}
		var answer *model.DeepAnswer
		for j := range contest {
			if contest[j].Target == id && contest[j].Verdict == "contest" {
				answer = &contest[j]
			}
		}
		if answer != nil && response != nil && response.Outcome == model.DeepHeld {
			fmt.Fprintf(&open, "- %s: the verifier holds %q (%s); the drafter contests: %s\n", id, finding.Verdict, response.Note, answer.Reason)
		}
	}
	writes := map[string]any{fieldDeepProposal: proposal, fieldDeepHunks: model.RenderDeepHunks(hunks), fieldDeepOpen: open.String()}
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
	fieldVerifyBase      = "verifyBase"
	fieldVerifyPages     = "verifyPages"
	fieldVerifyPageFull  = "verifyPageFull"
	fieldVerifyPageAt    = "verifyPageAt"
	fieldVerifyPage      = "pageText"
	fieldVerifyPageNote  = "pageNote"
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

func (w *WorkflowSession) showVerifyPage(store *engine.Store, at int) error {
	pages := workflowStoreStrings(store, fieldVerifyPages)
	if at >= len(pages) {
		return fmt.Errorf("page %d of %d does not exist", at+1, len(pages))
	}
	note := fmt.Sprintf("Page %d of %d.", at+1, len(pages))
	if at < len(pages)-1 {
		note += " Report more for the next page; your findings are refused until every page is served."
	} else {
		note += " Every page is served."
	}
	for name, value := range map[string]any{fieldVerifyPageAt: at, fieldVerifyPage: pages[at], fieldVerifyPageNote: note} {
		err := store.WriteEngine(name, value)
		if err != nil {
			return err
		}
	}
	return nil
}

// verifyPageFullIDs returns the entries the current page of the reading serves
// in full.
func verifyPageFullIDs(store *engine.Store) ([]string, error) {
	full, err := storeDeep[[]string](store, fieldVerifyPageFull)
	if err != nil {
		return nil, err
	}
	at, _ := verifyPageAt(store)
	if at >= len(full) {
		return nil, nil
	}
	return full[at], nil
}

func verifyPageAt(store *engine.Store) (int, int) {
	at, _ := store.Get(fieldVerifyPageAt)
	return deepRoundCount(at), len(workflowStoreStrings(store, fieldVerifyPages))
}

func (w *WorkflowSession) registerVerifyTask(registry *engine.Registry) error {
	// The deep exchange's documents are the exchange itself: a page, the
	// findings, the proposal and its hunks reach their reader whole, never at
	// the store-value cap.
	err := registry.RegisterQuery(engine.Query{
		Doc:       engine.FuncDoc{Name: "deepDocument", Doc: "Serves one deep-exchange store value whole: a reading page, the findings, the proposal or its hunks, up to one page of the reading."},
		ServeSafe: true,
		Bound:     engine.QueryBound{Part: serveview.PartText, Cap: serveview.Cap{MaxBytes: model.DeepPageChars}},
		Fn: func(ctx *engine.Context, args map[string]any) (any, error) {
			field, _ := args["field"].(string)
			value, _ := workflowStoreString(ctx.Store, field)
			return value, nil
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
	}
	for _, predicate := range predicates {
		err := registry.RegisterPredicate(predicate)
		if err != nil {
			return err
		}
	}
	commands := []engine.Command{
		{
			Doc: engine.FuncDoc{Name: "loadVerifyInput", Doc: "Loads the draft document and the paged reading from the capture this delegate works for, and serves page 1.", Writes: []string{fieldVerifyBase, fieldVerifyPages, fieldVerifyPageFull, fieldVerifyPageAt, fieldVerifyPage, fieldVerifyPageNote}},
			Fn: func(ctx *engine.Context) error {
				parent, err := w.dispatchingStore()
				if err != nil {
					return err
				}
				base, _ := workflowStoreString(parent, fieldDeepBase)
				pages := workflowStoreStrings(parent, fieldDeepPages)
				if base == "" || len(pages) == 0 {
					return errors.New("the dispatching capture holds no prepared deep exchange")
				}
				err = ctx.Store.WriteEngine(fieldVerifyBase, base)
				if err != nil {
					return err
				}
				err = ctx.Store.WriteEngine(fieldVerifyPages, pages)
				if err != nil {
					return err
				}
				full, err := storeDeep[[]string](parent, fieldDeepPageFull)
				if err != nil {
					return err
				}
				err = ctx.Store.WriteEngine(fieldVerifyPageFull, full)
				if err != nil {
					return err
				}
				return w.showVerifyPage(ctx.Store, 0)
			},
		},
		{
			Doc: engine.FuncDoc{Name: "turnPage", Doc: "Serves the next page of the reading.", Reads: []string{fieldVerifyPages, fieldVerifyPageAt}, Writes: []string{fieldVerifyPageAt, fieldVerifyPage, fieldVerifyPageNote}},
			Fn: func(ctx *engine.Context) error {
				at, count := verifyPageAt(ctx.Store)
				if at+1 >= count {
					return errors.New("every page is served: report judge with your findings")
				}
				return w.showVerifyPage(ctx.Store, at+1)
			},
		},
		{
			Doc: engine.FuncDoc{Name: "requireReadingServed", Doc: "Refuses findings until every page of the reading was served (V-D8).", Reads: []string{fieldVerifyPages, fieldVerifyPageAt}},
			Fn: func(ctx *engine.Context) error {
				at, count := verifyPageAt(ctx.Store)
				if at+1 < count {
					return fmt.Errorf("page %d of %d is the last served: report more until every page is served, then judge", at+1, count)
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
				problems := model.CheckDeepFindings(base, findings, func(id string) bool { return ctx.Graph != nil && ctx.Graph.ByID[id] != nil })
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
