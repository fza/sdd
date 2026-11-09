package engine

import (
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
	seen := map[string]bool{}
	var events []model.HookEvent
	add := func(event model.HookEvent) {
		if !seen[event.Name] {
			seen[event.Name] = true
			events = append(events, event)
		}
	}
	addStep := func(spec *Spec, step *Step, phase HookPhase, findings bool) {
		add(model.HookEvent{Name: StepEvent(spec.Canonical, step.ID, phase), Procedure: spec.Canonical, Step: step.ID, Phase: string(phase), Findings: findings})
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

func (s *Session) stepPoint(inst *Instance, step *Step, phase HookPhase, transition string) (HookPoint, error) {
	point := HookPoint{
		Event: StepEvent(inst.Spec.Canonical, step.ID, phase), Procedure: inst.Spec.Canonical,
		Instance: inst.ID, Step: step.ID, Phase: phase, Transition: transition, Store: inst.Store,
	}
	if step.Findings != "" {
		findings, err := channelFindings(inst.Store, step.Findings)
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
	_, err := s.fireStep(inst, inst.currentStep(), HookStart, "", nil)
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
	if _, err := s.fireStep(inst, inst.currentStep(), HookStart, "", nil); err != nil {
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

func (s *Session) fireStep(inst *Instance, step *Step, phase HookPhase, transition string, extra map[string]any) ([]query.Finding, error) {
	if s.hooks == nil {
		return nil, nil
	}
	point, err := s.stepPoint(inst, step, phase, transition)
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
	if step == nil {
		return false, nil
	}
	findings, err := s.fireStep(inst, step, HookDone, to, nil)
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

func highStepFindingsDetail(ctx *Context) string {
	high, err := highStepFindings(ctx)
	if err != nil || len(high) == 0 {
		return ""
	}
	lines := make([]string, 0, len(high))
	for _, finding := range high {
		lines = append(lines, fmt.Sprintf("[high] %s: %s", finding.Category, finding.Observation))
	}
	return "high findings on this step: " + strings.Join(lines, "; ") + ". Revise and report again."
}
