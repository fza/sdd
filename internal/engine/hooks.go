package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

// HookPhase is the moment of a step a hook event marks.
type HookPhase string

const (
	HookStart HookPhase = model.HookPhaseStart
	HookDone  HookPhase = model.HookPhaseDone
	HookRound HookPhase = model.HookPhaseRound
)

// HooksFn is the op_result function name under which the engine logs what
// hooks wrote into a findings channel, and a step held or released by its
// start hooks.
const HooksFn = "hooks"

// HookPoint is one moment the engine runs the configured hooks at
// (20261109-213250-d-tac-bve): a step's event, or an event a command
// announces, which carries no phase.
type HookPoint struct {
	Event     string
	Procedure string
	Instance  string
	Step      string
	Phase     HookPhase
	// Transition is the target selected as a step is done. A high finding
	// the hooks report may still hold the step or redirect the move.
	Transition string
	// Channel marks a step with a findings channel; Findings holds the
	// channel's findings so far.
	Channel  bool
	Findings []query.Finding
	// Extra holds the fields a round adds to its payload.
	Extra map[string]any
	Store *Store
}

// Hooks runs every configured hook matching a point's event and returns the
// findings they reported. An error fails the call that raised the event.
type Hooks interface {
	Fire(HookPoint) ([]query.Finding, error)
}

// WithHooks runs hooks at the session's events. Replay never raises one.
func WithHooks(hooks Hooks) SessionOption {
	return func(s *Session) { s.hooks = hooks }
}

// StepEvent names a step's hook event.
func StepEvent(procedure, step string, phase HookPhase) string {
	return model.StepHookEvent(procedure, step, string(phase))
}

// HookEvent returns the point's event in the form hook patterns match
// against; a command's event names no step.
func (p HookPoint) HookEvent() model.HookEvent {
	if p.Phase == "" {
		return model.HookEvent{Name: p.Event}
	}
	return model.HookEvent{Name: p.Event, Procedure: p.Procedure, Step: p.Step, Phase: string(p.Phase), Findings: p.Channel}
}

// HookEvents lists, sorted by name, every event the specs raise with the
// registry's commands: arriving at each step and its being done, a round at
// a step whose commands declare rounds, and each event a step's commands
// announce. A step with a findings channel being done is the one event
// accepting findings.
func HookEvents(specs []*Spec, reg *Registry) []model.HookEvent {
	seen := map[string]int{}
	var events []model.HookEvent
	add := func(event model.HookEvent) {
		if i, ok := seen[event.Name]; ok {
			events[i].Findings = events[i].Findings || event.Findings
			return
		}
		seen[event.Name] = len(events)
		events = append(events, event)
	}
	addStep := func(spec *Spec, step *Step, phase HookPhase, findings bool) {
		name := hookStep(step)
		add(model.HookEvent{Name: StepEvent(spec.Canonical, name, phase), Procedure: spec.Canonical, Step: name, Phase: string(phase), Findings: findings})
	}
	for _, spec := range specs {
		for _, step := range spec.Steps {
			addStep(spec, step, HookStart, false)
			addStep(spec, step, HookDone, step.Findings != "")
			names := []string{step.Op}
			for _, option := range step.Options {
				names = append(names, option.Call)
			}
			for _, name := range names {
				cmd, ok := reg.Command(name)
				if !ok {
					continue
				}
				if cmd.Rounds {
					addStep(spec, step, HookRound, false)
				}
				if cmd.Announces != "" {
					add(model.HookEvent{Name: cmd.Announces})
				}
			}
		}
	}
	slices.SortFunc(events, func(a, b model.HookEvent) int { return strings.Compare(a.Name, b.Name) })
	return events
}

// hookBase is a findings channel's value before hooks last joined it,
// restored before the step's gate runs again, on a held step or a later
// arrival, so the hooks judge afresh.
type hookBase struct {
	value   any
	present bool
}

// hookStep is the step name a step's hook events carry: its stage, else its
// id.
func hookStep(step *Step) string {
	if step.Stage != "" {
		return step.Stage
	}
	return step.ID
}

// withinStage reports whether moving from step to the target stays inside
// the step's stage, which raises no start or done event.
func withinStage(spec *Spec, step *Step, to string) bool {
	target := spec.StepByID[to]
	return step != nil && step.Stage != "" && target != nil && target.Stage == step.Stage
}

func (s *Session) stepPoint(inst *Instance, step *Step, store *Store, phase HookPhase, transition string) (HookPoint, error) {
	point := HookPoint{
		Event: StepEvent(inst.Spec.Canonical, hookStep(step), phase), Procedure: inst.Spec.Canonical,
		Instance: inst.ID, Step: hookStep(step), Phase: phase, Transition: transition, Store: store,
	}
	if step.Findings != "" {
		findings, err := channelFindings(store, step.Findings)
		if err != nil {
			return HookPoint{}, err
		}
		point.Channel, point.Findings = true, findings
	}
	return point, nil
}

func (s *Session) fire(point HookPoint) ([]query.Finding, error) {
	if s.hooks == nil {
		return nil, nil
	}
	findings, err := s.hooks.Fire(point)
	if err != nil {
		return nil, fmt.Errorf("hooks at %s: %w", point.Event, err)
	}
	if len(findings) > 0 && (point.Phase != HookDone || !point.Channel) {
		return nil, fmt.Errorf("hooks at %s returned findings, but only a step with a findings channel being done accepts them", point.Event)
	}
	return findings, nil
}

// arrive raises the current step's start event. A failing hook holds the
// step, logged so a replayed session holds it too: every later call raises
// the event again before the step's work runs (release).
func (s *Session) arrive(inst *Instance) error {
	_, err := s.fireStep(inst, inst.currentStep(), inst.Store, HookStart, "", nil)
	if err == nil {
		return nil
	}
	inst.startHeld = true
	s.appendEvent(inst.ID, EventOpResult, map[string]any{"step": inst.Step, "fn": HooksFn, "start": hookStartHeld})
	return errors.Join(fmt.Errorf("step %s is held until its start hooks pass: %w", inst.Step, err), s.checkSink())
}

// release raises a held step's start event again and lets the step's work
// run once the hooks pass.
func (s *Session) release(inst *Instance) error {
	if !inst.startHeld {
		return nil
	}
	if _, err := s.fireStep(inst, inst.currentStep(), inst.Store, HookStart, "", nil); err != nil {
		return fmt.Errorf("step %s is held until its start hooks pass: %w", inst.Step, err)
	}
	inst.startHeld = false
	s.appendEvent(inst.ID, EventOpResult, map[string]any{"step": inst.Step, "fn": HooksFn, "start": hookStartPassed})
	return s.checkSink()
}

const (
	hookStartHeld   = "held"
	hookStartPassed = "passed"
)

func (s *Session) fireStep(inst *Instance, step *Step, store *Store, phase HookPhase, transition string, extra map[string]any) ([]query.Finding, error) {
	if s.hooks == nil {
		return nil, nil
	}
	point, err := s.stepPoint(inst, step, store, phase, transition)
	if err != nil {
		return nil, err
	}
	point.Extra = extra
	return s.fire(point)
}

// leave raises the current step's done event and joins the findings the
// hooks report into its channel, reporting whether any joined.
func (s *Session) leave(inst *Instance, to string) (bool, error) {
	step := inst.currentStep()
	if step == nil || withinStage(inst.Spec, step, to) {
		return false, nil
	}
	findings, err := s.fireStep(inst, step, inst.Store, HookDone, to, nil)
	if err != nil || len(findings) == 0 {
		return false, err
	}
	current, err := channelFindings(inst.Store, step.Findings)
	if err != nil {
		return false, err
	}
	base, present := inst.Store.Get(step.Findings)
	if err := inst.Store.WriteEngine(step.Findings, append(current, findings...)); err != nil {
		return false, err
	}
	joined, _ := inst.Store.Get(step.Findings)
	if inst.hookBase == nil {
		inst.hookBase = map[string]hookBase{}
	}
	inst.hookBase[step.ID] = hookBase{value: base, present: present}
	data := map[string]any{"step": step.ID, "fn": HooksFn, "writes": map[string]any{step.Findings: joined}, "joined": true}
	if present {
		data["base"] = base
	}
	s.appendEvent(inst.ID, EventOpResult, data)
	return true, s.checkSink()
}

// restoreHookFindings drops what hooks joined on the step's last attempt.
func (s *Session) restoreHookFindings(inst *Instance, step *Step) error {
	base, ok := inst.hookBase[step.ID]
	if !ok {
		return nil
	}
	delete(inst.hookBase, step.ID)
	var value any
	if base.present {
		value = base.value
	}
	if err := inst.Store.WriteEngine(step.Findings, value); err != nil {
		return err
	}
	s.appendEvent(inst.ID, EventOpResult, map[string]any{"step": step.ID, "fn": HooksFn, "writes": map[string]any{step.Findings: value}})
	return s.checkSink()
}

// replayHookFindings restores the bookkeeping of a logged hooks op_result:
// a held or released start, or a join recording the base and a restore
// clearing it.
func replayHookFindings(inst *Instance, ev Event) {
	if start, ok := ev.Data["start"].(string); ok {
		inst.startHeld = start == hookStartHeld
		return
	}
	step, _ := ev.Data["step"].(string)
	if joined, _ := ev.Data["joined"].(bool); !joined {
		delete(inst.hookBase, step)
		return
	}
	if inst.hookBase == nil {
		inst.hookBase = map[string]hookBase{}
	}
	base, present := ev.Data["base"]
	inst.hookBase[step] = hookBase{value: base, present: present}
}

func channelFindings(store *Store, field string) ([]query.Finding, error) {
	v, ok := store.Get(field)
	if !ok || v == nil {
		return []query.Finding{}, nil
	}
	normalized, err := VarType{Base: TypePreflightFindings}.ValidateValue(v)
	if err != nil {
		return nil, fmt.Errorf("findings channel %s has unexpected shape: %w", field, err)
	}
	return normalized.([]query.Finding), nil
}

// noHighStepFindings passes unless the current step's findings channel holds
// a high finding.
func noHighStepFindings(ctx *Context) (bool, error) {
	high, err := highStepFindings(ctx)
	return len(high) == 0, err
}

func highStepFindings(ctx *Context) ([]query.Finding, error) {
	step := ctx.Store.spec.StepByID[ctx.Step]
	if step == nil || step.Findings == "" {
		return nil, nil
	}
	findings, err := channelFindings(ctx.Store, step.Findings)
	if err != nil {
		return nil, err
	}
	var high []query.Finding
	for _, finding := range findings {
		if finding.Severity == query.SeverityHigh {
			high = append(high, finding)
		}
	}
	return high, nil
}

// fieldHookFindingsWeighed holds, per findings channel, the findings the
// agent last weighed there (recordHookReview).
const fieldHookFindingsWeighed = "hookFindingsWeighed"

// noHookFindings passes unless the hooks joined findings into the current
// step's channel when it was last left.
func noHookFindings(ctx *Context) (bool, error) {
	return !ctx.hooksJoined, nil
}

// hookFindingsWeighed passes when the current step's findings channel holds
// exactly the findings the agent last weighed there.
func hookFindingsWeighed(ctx *Context) (bool, error) {
	step := ctx.Store.spec.StepByID[ctx.Step]
	if step == nil || step.Findings == "" {
		return false, nil
	}
	current, err := channelFindings(ctx.Store, step.Findings)
	if err != nil {
		return false, err
	}
	weighed, _ := ctx.Store.Get(fieldHookFindingsWeighed)
	record, _ := weighed.(map[string]any)
	recorded, ok := record[step.Findings]
	if !ok {
		return false, nil
	}
	return findingsJSON(current) == findingsJSON(recorded), nil
}

// recordHookReview records each findings channel's current findings as
// weighed by the agent.
func recordHookReview(ctx *Context) error {
	record := map[string]any{}
	for _, step := range ctx.Store.spec.Steps {
		if step.Findings == "" {
			continue
		}
		findings, err := channelFindings(ctx.Store, step.Findings)
		if err != nil {
			return err
		}
		if len(findings) > 0 {
			record[step.Findings] = findings
		}
	}
	return ctx.Store.WriteEngine(fieldHookFindingsWeighed, record)
}

func findingsJSON(findings any) string {
	normalized, err := VarType{Base: TypePreflightFindings}.ValidateValue(findings)
	if err != nil {
		return ""
	}
	encoded, _ := json.Marshal(normalized)
	return string(encoded)
}

func highStepFindingsDetail(ctx *Context) string {
	high, err := highStepFindings(ctx)
	if err != nil || len(high) == 0 {
		return ""
	}
	lines := make([]string, 0, len(high))
	for _, finding := range high {
		lines = append(lines, fmt.Sprintf("[high] %s: %s", finding.Category, finding.Observation))
	}
	return "high findings on this step: " + strings.Join(lines, "; ") + ". Revise, then report again."
}
