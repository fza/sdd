package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/pkg/application/types"
)

// Hook is one declared hook resolved against its layer
// (20261109-213250-d-tac-bve): a file run with an event's JSON payload on
// stdin, its hash, and whether this machine confirmed it.
type Hook = types.Hook

// HookReview is what the person sees before confirming a hook.
type HookReview = types.HookReview

// HookCommands is the composition's port to the declared hooks. Hooks
// returns them as declared at the time of the call, each with its state, so
// a changed configuration or file reaches the next call. Run runs one hook
// with stdin and returns what it wrote to stdout; it stops the command when
// ctx ends, and a nonzero exit is an error. Review describes a hook for the
// person confirming it, and Confirm records its hash.
type HookCommands interface {
	Hooks(ctx context.Context) ([]Hook, error)
	Run(ctx context.Context, hook Hook, stdin []byte) ([]byte, error)
	Review(ctx context.Context, hook Hook) (HookReview, error)
	Confirm(ctx context.Context, hook Hook) error
}

// HookConfirmer asks the person running sdd whether a hook not confirmed on
// this machine may run, showing them review. An error means the person
// could not be asked.
type HookConfirmer interface {
	ConfirmHook(ctx context.Context, review HookReview) (bool, error)
}

type hookConfirmerKey struct{}

// WithHookConfirmer returns ctx carrying the confirmer a call reaching an
// unconfirmed hook asks; without one such a call fails.
func WithHookConfirmer(ctx context.Context, confirmer HookConfirmer) context.Context {
	return context.WithValue(ctx, hookConfirmerKey{}, confirmer)
}

// workflowHooks runs a workflow session's hooks: every hook matching an
// event at once and to completion, merging their findings, and failing the
// event once all finished when any failed.
type workflowHooks struct {
	workflow *WorkflowSession
	resolved bool
	commands HookCommands
	hooks    []Hook
	err      error
}

// resolve reads the configured hooks once per session load and checks them
// against the events the project's procedures raise.
func (h *workflowHooks) resolve() error {
	if h.resolved {
		return h.err
	}
	h.resolved = true
	w := h.workflow
	_, runtime, err := w.app.resolve(w.ctx, w.identity, w.project, AccessRead)
	if err != nil {
		h.err = err
		return err
	}
	h.commands = runtime.options.Hooks
	if h.commands == nil {
		return nil
	}
	if h.hooks, err = h.commands.Hooks(w.ctx); err != nil {
		h.err = fmt.Errorf("reading the configured hooks: %w", err)
		return h.err
	}
	if len(h.hooks) == 0 {
		return nil
	}
	events, err := w.hookEvents()
	if err != nil {
		h.err = err
		return err
	}
	if err := model.ValidateHooks(h.hooks, events); err != nil {
		h.err = fmt.Errorf("configured hooks: %w", err)
	}
	return h.err
}

func (h *workflowHooks) Fire(point engine.HookPoint) ([]query.Finding, error) {
	if err := h.resolve(); err != nil {
		return nil, err
	}
	event := point.HookEvent()
	var (
		matched    []Hook
		unanswered []error
	)
	for i := range h.hooks {
		if !model.MatchHookEvent(h.hooks[i].Event, event) {
			continue
		}
		if err := h.confirm(&h.hooks[i]); err != nil {
			unanswered = append(unanswered, err)
			continue
		}
		matched = append(matched, h.hooks[i])
	}
	if err := errors.Join(unanswered...); err != nil {
		return nil, err
	}
	if len(matched) == 0 {
		return nil, nil
	}
	fields, err := h.workflow.hookPayload(point)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	findings := make([][]query.Finding, len(matched))
	errs := make([]error, len(matched))
	var wg sync.WaitGroup
	for i, hook := range matched {
		wg.Go(func() {
			findings[i], errs[i] = runHook(h.workflow.ctx, h.commands, hook, payload)
			if errs[i] != nil {
				errs[i] = fmt.Errorf("%s: %w", hook.Label(), errs[i])
			}
		})
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	var merged []query.Finding
	for _, reported := range findings {
		merged = append(merged, reported...)
	}
	return merged, nil
}

// confirm lets a hook this machine has not confirmed run once the person,
// asked through the call's confirmer, accepts it; a decline, or a call
// without a confirmer, fails naming `sdd hooks trust`.
func (h *workflowHooks) confirm(hook *Hook) error {
	if hook.State == model.HookConfirmed {
		return nil
	}
	refusal := fmt.Errorf("%s is %s on this machine; review and confirm it with `sdd hooks trust`", hook.Label(), hook.State)
	ctx := h.workflow.ctx
	confirmer, _ := ctx.Value(hookConfirmerKey{}).(HookConfirmer)
	if confirmer == nil {
		return refusal
	}
	review, err := h.commands.Review(ctx, *hook)
	if err != nil {
		return errors.Join(refusal, err)
	}
	accepted, err := confirmer.ConfirmHook(ctx, review)
	if err != nil {
		return fmt.Errorf("%w (asking failed: %w)", refusal, err)
	}
	if !accepted {
		return fmt.Errorf("%w (declined)", refusal)
	}
	if err := h.commands.Confirm(ctx, *hook); err != nil {
		return err
	}
	hook.State = model.HookConfirmed
	return nil
}

func runHook(ctx context.Context, commands HookCommands, hook Hook, payload []byte) ([]query.Finding, error) {
	timeout := hook.TimeoutDuration()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	output, err := commands.Run(ctx, hook, payload)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("timed out after %s", timeout)
	}
	if err != nil {
		return nil, err
	}
	return parseHookOutput(output, hook.Findings)
}

// parseHookOutput reads a hook's stdout: nothing, or `{"findings": [...]}`
// in pre-flight's finding format.
func parseHookOutput(output []byte, declaresFindings bool) ([]query.Finding, error) {
	output = bytes.TrimSpace(output)
	if len(output) == 0 {
		return nil, nil
	}
	var answer struct {
		Findings []Finding `json:"findings"`
	}
	if err := json.Unmarshal(output, &answer); err != nil {
		return nil, fmt.Errorf(`output is neither empty nor {"findings": [...]}: %w`, err)
	}
	if len(answer.Findings) > 0 && !declaresFindings {
		return nil, fmt.Errorf("answered with findings, but the hook does not declare findings")
	}
	findings := make([]query.Finding, 0, len(answer.Findings))
	for _, finding := range answer.Findings {
		switch query.Severity(finding.Severity) {
		case query.SeverityHigh, query.SeverityMedium, query.SeverityLow:
		default:
			return nil, fmt.Errorf("finding severity %q is not high, medium or low", finding.Severity)
		}
		findings = append(findings, query.Finding{Severity: query.Severity(finding.Severity), Category: finding.Category, Observation: finding.Observation})
	}
	return findings, nil
}

// hookEvents lists the events the procedures of the session's graph raise.
// A procedure that does not load cannot run, so it raises none.
func (w *WorkflowSession) hookEvents() ([]model.HookEvent, error) {
	graph, err := w.graphs.Current()
	if err != nil {
		return nil, fmt.Errorf("loading graph: %w", err)
	}
	seen := map[string]bool{}
	var specs []*engine.Spec
	for _, entry := range graph.Procedures() {
		if seen[entry.Canonical] {
			continue
		}
		seen[entry.Canonical] = true
		spec, err := engine.LoadSpec(graph.ResolveProcedure(entry.Canonical), w.engine.Registry)
		if err != nil {
			continue
		}
		specs = append(specs, spec)
	}
	return engine.HookEvents(specs, w.engine.Registry), nil
}

// hookPayload is the JSON object a hook reads on stdin. Readers ignore
// fields they do not know, so new fields need no version.
func (w *WorkflowSession) hookPayload(point engine.HookPoint) (map[string]any, error) {
	project := w.instanceProject(point.Instance)
	target, _ := w.effectiveTargetFor(project, point.Store)
	if target.Branch == "" {
		runtime, err := w.targetRuntime(project, AccessRead)
		if err != nil {
			return nil, err
		}
		if target, _, err = runtime.baseTarget(w.ctx); err != nil {
			return nil, err
		}
	}
	payload := map[string]any{}
	for name, value := range point.Extra {
		payload[name] = value
	}
	for name, value := range map[string]any{
		"event": point.Event, "time": w.app.now().UTC().Format(time.RFC3339),
		"project": string(project), "branch": target.Branch, "session": string(w.ID()),
		"procedure": point.Procedure, "instance": point.Instance, "step": point.Step,
	} {
		payload[name] = value
	}
	if point.Phase != "" {
		payload["phase"] = string(point.Phase)
	}
	if point.Transition != "" {
		payload["transition"] = point.Transition
	}
	if point.Channel {
		payload["findings"] = point.Findings
	}
	if point.Procedure == captureCanonical {
		payload["draft"] = hookDraft(point.Store)
	}
	if entryID, ok := workflowStoreString(point.Store, "entryId"); ok && point.Event == entryWrittenEvent {
		payload["entry"] = entryID
	}
	return payload, nil
}

const (
	captureCanonical  = "capture"
	entryWrittenEvent = "entry.written"
)

func hookDraft(store *engine.Store) map[string]any {
	kind, _ := workflowStoreString(store, "entryKind")
	layer, _ := workflowStoreString(store, "layer")
	body, _ := workflowStoreString(store, "body")
	entryType, _ := entryTypeForKind(model.Kind(kind))
	refs, _ := store.Get("refs")
	hash := sha256.Sum256([]byte(body))
	return map[string]any{
		"type": string(entryType), "kind": kind, "layer": layer, "refs": refs,
		"topics": workflowStoreStrings(store, "topics"), "body": body,
		"attachments": workflowStoreStrings(store, "attachments"), "body_hash": hex.EncodeToString(hash[:]),
	}
}
