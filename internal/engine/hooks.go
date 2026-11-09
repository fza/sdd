package engine

import (
	"fmt"
	"slices"
	"strings"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

// HookPhase is the moment of a step a hook event marks.
type HookPhase string

const (
	HookEntered HookPhase = "entered"
	HookLeft    HookPhase = "left"
	HookRound   HookPhase = "round"
)

// HooksFn is the op_result function name under which the engine logs what
// hooks wrote into a findings channel.
const HooksFn = "hooks"

// HookPoint is one moment the engine runs the configured hooks at
// (20261109-005138-d-tac-i61): a step event `<procedure>.<step>.<phase>`,
// or an event a command announces, which carries no phase.
type HookPoint struct {
	Event     string
	Procedure string
	Instance  string
	Step      string
	Phase     HookPhase
	// Transition is the target selected on leaving a step. A high finding
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
	return procedure + "." + step + "." + string(phase)
}

// HookEvents lists, sorted by name, every event the specs raise with the
// registry's commands: entering and leaving each step, a round at a step
// whose commands declare rounds, and each event a step's commands announce.
// Leaving a step with a findings channel is the one event accepting findings.
func HookEvents(specs []*Spec, reg *Registry) []model.HookEvent {
	seen := map[string]bool{}
	var events []model.HookEvent
	add := func(name string, findings bool) {
		if !seen[name] {
			seen[name] = true
			events = append(events, model.HookEvent{Name: name, Findings: findings})
		}
	}
	for _, spec := range specs {
		for _, step := range spec.Steps {
			add(StepEvent(spec.Canonical, step.ID, HookEntered), false)
			add(StepEvent(spec.Canonical, step.ID, HookLeft), step.Findings != "")
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
					add(StepEvent(spec.Canonical, step.ID, HookRound), false)
				}
				if cmd.Announces != "" {
					add(cmd.Announces, false)
				}
			}
		}
	}
	slices.SortFunc(events, func(a, b model.HookEvent) int { return strings.Compare(a.Name, b.Name) })
	return events
}

// hookBase is a findings channel's value before hooks joined it, restored
// before the step's gate runs again so a held step judges afresh.
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
	if len(findings) > 0 && (point.Phase != HookLeft || !point.Channel) {
		return nil, fmt.Errorf("hooks at %s returned findings, but only leaving a step with a findings channel accepts them", point.Event)
	}
	return findings, nil
}

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

// leave raises the current step's left event and joins the findings the
// hooks report into its channel, reporting whether any joined.
func (s *Session) leave(inst *Instance, to string) (bool, error) {
	step := inst.currentStep()
	if step == nil {
		return false, nil
	}
	findings, err := s.fireStep(inst, step, HookLeft, to, nil)
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

// replayHookFindings restores the join bookkeeping from a logged hooks
// op_result: a join records the base, a restore clears it.
func replayHookFindings(inst *Instance, ev Event) {
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
