package engine

import (
	"fmt"

	"github.com/networkteam/sdd/internal/model"
)

// An assisted session (model.DialogueAssisted) answers every user chooser as
// an autonomous one does, except its approval gate: the step whose option
// confirms playback. There the person answers, the confirmation records their
// approval bound to the state snapshot, and the instance is held for them on
// arrival, so a session ending unanswered leaves it parked at the gate.

const approvalHoldReason = "the checked draft waits for the person's approval"

// approvalGateAt reports whether step is inst's approval gate in force.
func (s *Session) approvalGateAt(inst *Instance, step *Step) bool {
	return s.DialogueMode == model.DialogueAssisted && step.Chooser == ChooserUser && step.ID == confirmingStep(inst.Spec)
}

func approvalGateRefusal(step *Step) error {
	return fmt.Errorf("%s is the person's approval in an assisted session: play the checked draft back to them and relay their own answer verbatim; end your turn until they give one", step.ID)
}

// holdForApproval parks inst at its approval gate for the person, logging it
// once per arrival; the serve still offers the chooser.
func (s *Session) holdForApproval(inst *Instance, step *Step) {
	if inst.parking != nil && inst.parking.Step == step.ID {
		return
	}
	inst.parking = &Parking{Step: step.ID, Reason: approvalHoldReason, Since: s.now()}
	s.appendEvent(inst.ID, EventParked, map[string]any{"step": step.ID, "reason": approvalHoldReason, "held": true})
}

// Approved reports whether store holds a person's approval of its current
// state, given at an assisted session's approval gate.
func Approved(store *Store) bool {
	v, _ := store.Get(fieldPlaybackConfirmation)
	conf, ok := asConfirmation(v)
	return ok && conf.Approval && conf.Snapshot == store.StateSnapshot()
}

// Sealed lists the running instances that wait at their approval gate, in
// start order: the drafts one answer of the person may settle together.
func (s *Session) Sealed() []string {
	var sealed []string
	for _, inst := range s.Instances() {
		if s.sealed(inst, "") == nil {
			sealed = append(sealed, inst.ID)
		}
	}
	return sealed
}

// CheckSealed refuses instanceID joining one person's answer at chooser
// unless it waits at that approval gate with its draft played back.
func (s *Session) CheckSealed(instanceID, chooser string) error {
	inst, ok := s.instances[instanceID]
	if !ok {
		return fmt.Errorf("instance %q not found in session", instanceID)
	}
	return s.sealed(inst, chooser)
}

func (s *Session) sealed(inst *Instance, chooser string) error {
	if inst.Status != StatusRunning {
		return fmt.Errorf("%s is not sealed: it has ended (%s)", inst.ID, inst.Outcome)
	}
	step := inst.currentStep()
	switch {
	case step == nil || !s.approvalGateAt(inst, step):
		return fmt.Errorf("%s is not sealed: it does not wait at an approval gate of an assisted session", inst.ID)
	case chooser != "" && step.ID != chooser:
		return fmt.Errorf("%s is not sealed at %s: it waits at %s", inst.ID, chooser, step.ID)
	case draftChangedUnserved(inst, step):
		return fmt.Errorf("%s is not sealed: its draft changed since it was played back", inst.ID)
	case s.PendingMutation() != nil && s.PendingMutation().Instance == inst.ID:
		return fmt.Errorf("%s is not sealed: an operation is pending", inst.ID)
	}
	return nil
}
