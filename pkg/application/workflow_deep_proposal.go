package application

import (
	"fmt"
	"maps"
	"slices"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

const commandAcceptDeepProposal = "acceptDeepProposal"

// applyDeepProposal writes the deep verifier's proposal into an accept that
// reports no draft field, as the fields the proposal changes, so the engine
// validates and writes them like reported ones and the answer's log holds
// them. An accept reporting draft fields is left to match the proposal.
func (w *WorkflowSession) applyDeepProposal(request WorkflowAdvanceRequest) error {
	inst, ok := w.session.Instance(request.Instance)
	if !ok || inst.Status != engine.StatusRunning {
		return nil
	}
	chooser, _ := request.Report["chooser"].(string)
	choice, _ := request.Report["choice"].(string)
	option := answeredOption(inst.Spec, chooser, choice)
	if option == nil || option.Call != commandAcceptDeepProposal {
		return nil
	}
	fields, _ := request.Report["fields"].(map[string]any)
	collected := make([]string, 0, len(option.Collect))
	for _, field := range option.Collect {
		collected = append(collected, field.Name)
		if _, reported := fields[field.Name]; reported {
			return nil
		}
	}
	proposal, _ := workflowStoreString(inst.Store, fieldDeepProposal)
	changed, err := deepProposalFields(inst.Store, proposal)
	if err != nil {
		return err
	}
	if len(changed) == 0 {
		return nil
	}
	if _, rewritten := changed[fieldBody]; rewritten && inst.Store.Has(fieldBodyHandle) {
		bound, _ := workflowStoreString(inst.Store, fieldBodyHandle)
		return fmt.Errorf("the body comes from the staged file %q: stage the proposal's body there with stage_attachment, then accept", bound)
	}
	for _, name := range slices.Sorted(maps.Keys(changed)) {
		if !slices.Contains(collected, name) {
			return fmt.Errorf("the proposal changes %s, which accept cannot write: contest the finding whose edit changes it, or escalate", name)
		}
	}
	if fields == nil {
		fields = map[string]any{}
		request.Report["fields"] = fields
	}
	maps.Copy(fields, changed)
	return nil
}

// deepProposalFields returns the draft fields proposal sets otherwise than
// the store holds them, nil for a field the proposal drops.
func deepProposalFields(store *engine.Store, proposal string) (map[string]any, error) {
	parsed, err := model.ParseDeepDraftDocument(proposal)
	if err != nil {
		return nil, fmt.Errorf("the proposal no longer reads as a draft (%w): report the fields so the draft matches it", err)
	}
	for name := range parsed.Fields {
		if name == fieldBody || !slices.Contains(guideDraftFields, name) {
			return nil, fmt.Errorf("the proposal names %q, which is no draft field: report the fields so the draft matches it", name)
		}
	}
	changed := map[string]any{}
	for _, name := range guideDraftFields {
		current, _ := store.Get(name)
		var proposed any
		if name == fieldBody {
			if parsed.HasBody {
				proposed = parsed.Body
			}
		} else {
			proposed = parsed.Fields[name]
		}
		same, err := sameDraftValue(current, proposed)
		if err != nil {
			return nil, err
		}
		if !same {
			changed[name] = proposed
		}
	}
	return changed, nil
}

func sameDraftValue(a, b any) (bool, error) {
	if a == nil || b == nil {
		return a == nil && b == nil, nil
	}
	left, err := draftJSON(a)
	if err != nil {
		return false, err
	}
	right, err := draftJSON(b)
	return left == right, err
}

func answeredOption(spec *engine.Spec, chooser, choice string) *engine.Option {
	step := spec.StepByID[chooser]
	if step == nil {
		return nil
	}
	for i := range step.Options {
		if step.Options[i].Choice == choice {
			return &step.Options[i]
		}
	}
	return nil
}
