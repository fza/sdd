package engine

import (
	"fmt"
	"strconv"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

// Parking is where an autonomous session parked an instance: the step a
// person answers after resuming, the one-line reason, and since when.
type Parking struct {
	Step   string
	Reason string
	Since  time.Time
}

// unattendedAt returns the step's unattended bound when it is in force: at a
// user chooser in an autonomous session.
func (s *Session) unattendedAt(step *Step) *Unattended {
	if !s.DialogueMode.AgentAnswers() || step.Chooser != ChooserUser {
		return nil
	}
	return step.Unattended
}

func (inst *Instance) unattendedSpent(step *Step) bool {
	u := step.Unattended
	return u != nil && (u.Park != "" || u.Limit > 0 && inst.unattended[step.ID] >= u.Limit)
}

// personAnswersAt reports whether an assisted session holds inst at step for
// its person: an unattended step the agent spent. The person is present in
// assisted mode, so the step is served and answered as in a guided session
// instead of parking until a guided session resumes it.
func (s *Session) personAnswersAt(inst *Instance, step *Step) bool {
	return s.DialogueMode == model.DialogueAssisted && s.unattendedAt(step) != nil && inst.unattendedSpent(step)
}

// modeAt is the dialogue mode inst's step is served in.
func (s *Session) modeAt(inst *Instance, step *Step) model.DialogueMode {
	if step != nil && s.personAnswersAt(inst, step) {
		return model.DialogueGuided
	}
	return s.dialogueMode()
}

// parkReason is why an autonomous session parks inst at step: the park
// predicate's failure message, or the spent limit.
func (s *Session) parkReason(inst *Instance, step *Step) (string, error) {
	u := step.Unattended
	if u.Park == "" {
		return fmt.Sprintf("blocked again after %s unattended %s", strconv.Itoa(u.Limit), u.Answer), nil
	}
	p, found := s.engine.Registry.Predicate(u.Park)
	if !found {
		return "", fmt.Errorf("step %s parks on unknown predicate %q", step.ID, u.Park)
	}
	ctx, err := s.funcContext(inst)
	if err != nil {
		return "", err
	}
	if p.FailDetail != nil {
		if detail := p.FailDetail(ctx); detail != "" {
			return detail, nil
		}
	}
	return p.FailMessage, nil
}

// checkUnattended refuses the answers an unattended step forbids an
// autonomous session: a parked step, user words, any choice but its answer.
// A step held for an assisted session's person takes only their words.
func (s *Session) checkUnattended(inst *Instance, step *Step, choice, userWords string) error {
	u := s.unattendedAt(step)
	switch {
	case u == nil:
		return nil
	case s.personAnswersAt(inst, step) && userWords == "":
		return fmt.Errorf("%s is the person's now that the agent's unattended answers are spent: put it to them through %s and relay their answer verbatim as userWords; end your turn until they give one", step.ID, s.HowToAsk())
	case s.personAnswersAt(inst, step):
		return nil
	case inst.unattendedSpent(step):
		return fmt.Errorf("%s is parked: a person answers it after resume_session %s in a session whose dialogue.mode is guided or assisted (sdd config set --local dialogue.mode guided)", step.ID, s.ID)
	case userWords != "":
		return fmt.Errorf("%s refuses user words in an autonomous session (%s)", step.ID, unattendedRule(u))
	case choice != u.Answer:
		return fmt.Errorf("%s refuses %q in an autonomous session (%s)", step.ID, choice, unattendedRule(u))
	}
	return nil
}

// RefuseUnattendedAbandon refuses abandoning a running instance that waits at
// an unattended step of an autonomous session: dropping a blocked move is the
// user's, as overriding its check is, so an agent alone parks it instead. A
// draft at an assisted session's approval gate is the person's to drop too.
func (s *Session) RefuseUnattendedAbandon(instanceID string) error {
	inst, ok := s.instances[instanceID]
	if !ok || inst.Status != StatusRunning {
		return nil
	}
	step := inst.currentStep()
	if step != nil && s.approvalGateAt(inst, step) {
		return fmt.Errorf("%s at %s is not abandoned by the agent: it waits for the person's approval, and the person drops it by answering abort", instanceID, step.ID)
	}
	if step == nil || s.unattendedAt(step) == nil {
		return nil
	}
	if s.personAnswersAt(inst, step) {
		return fmt.Errorf("%s at %s is not abandoned by the agent: it waits for the person's answer, and the person drops it by answering abort", instanceID, step.ID)
	}
	return fmt.Errorf("%s at %s is not abandoned in an autonomous session: a person drops it, after resume_session %s in a session whose dialogue.mode is guided or assisted; until then it parks", instanceID, step.ID, s.ID)
}

func unattendedRule(u *Unattended) string {
	rule := fmt.Sprintf("unattended rule: only %q with your reason", u.Answer)
	if u.Limit > 0 {
		rule += ", " + timesWord(u.Limit) + "; the next block parks the move"
	}
	return rule + "; bypassing a check takes a person"
}

func timesWord(n int) string {
	if n == 1 {
		return "once"
	}
	return strconv.Itoa(n) + " times"
}

// countUnattended folds an answer into the step's count: the agent's answer
// adds one, a person's resets it.
func (inst *Instance) countUnattended(step *Step, answeredBy, userWords string) {
	inst.parking = nil
	if step == nil || step.Unattended == nil {
		return
	}
	switch {
	case answeredBy == "agent":
		if inst.unattended == nil {
			inst.unattended = map[string]int{}
		}
		inst.unattended[step.ID]++
	case userWords != "":
		delete(inst.unattended, step.ID)
	}
}

// hold parks inst at step for a person, logging it once per arrival.
func (s *Session) hold(inst *Instance, step *Step) error {
	if inst.parking != nil && inst.parking.Step == step.ID {
		return nil
	}
	reason, err := s.parkReason(inst, step)
	if err != nil {
		return err
	}
	inst.parking = &Parking{Step: step.ID, Reason: reason, Since: s.now()}
	s.appendEvent(inst.ID, EventParked, map[string]any{"step": step.ID, "reason": reason, "held": true})
	return nil
}

// heldForPersonLane tells the agent how the person answers a step an
// assisted session holds for them.
func (s *Session) heldForPersonLane(inst *Instance, step *Step) ServeLane {
	return ServeLane{Name: "parked", Text: fmt.Sprintf("Held at %s for the person: %s. Put this choice to them through %s and relay their answer verbatim as `userWords`; your own reason no longer passes here. No answer yet? End your turn: the move stays parked, and resume_session %s serves it again.", step.ID, inst.parking.Reason, s.HowToAsk(), s.ID)}
}

// park parks inst at step for a person, serving the agent nothing to answer.
func (s *Session) park(inst *Instance, step *Step, sv *Serve) error {
	if err := s.hold(inst, step); err != nil {
		return err
	}
	parked := *inst.parking
	sv.Parked = &parked
	sv.Goal = "end your turn; the move waits for a person"
	sv.Instructions = fmt.Sprintf("Parked at %s: %s. Nothing is written. End your turn with: parked at %s: resume_session %s", step.ID, parked.Reason, step.ID, s.ID)
	sv.Sizes = []PartSize{{Part: "parked", Bytes: len(sv.Instructions)}}
	return nil
}

func (s *Session) replayParked(ev Event) error {
	if held, _ := ev.Data["held"].(bool); !held {
		return nil
	}
	inst, err := s.replayInstance(ev)
	if err != nil {
		return err
	}
	step, _ := ev.Data["step"].(string)
	reason, _ := ev.Data["reason"].(string)
	inst.parking = &Parking{Step: step, Reason: reason, Since: ev.TS}
	if sealed, ok := ev.Data["snapshot"].(string); ok {
		inst.sealed = sealed
	}
	return nil
}
