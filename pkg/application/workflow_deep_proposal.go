package application

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

const commandAcceptDeepProposal = "acceptDeepProposal"

// applyDeepProposal merges the deep verifier's proposal into an accept's
// fields, so the engine validates and writes them like reported ones and the
// answer's log holds them: the fields the proposal changes take its values,
// and the reported ones stand beside them. A reported field the proposal
// changes otherwise is refused. A proposal rewriting the body writes it, the
// reported bodyPatch applied over it.
func (w *WorkflowSession) applyDeepProposal(request WorkflowAdvanceRequest) error {
	const fieldPatch = "bodyPatch"
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
	proposal, _ := workflowStoreString(inst.Store, fieldDeepProposal)
	changed, err := deepProposalFields(inst.Store, proposal)
	if err != nil {
		return err
	}
	if len(changed) == 0 {
		return nil
	}
	for _, name := range slices.Sorted(maps.Keys(changed)) {
		if !slices.ContainsFunc(option.Collect, func(field engine.CollectField) bool { return field.Name == name }) {
			return fmt.Errorf("the proposal changes %s, which accept cannot write: contest the finding whose edit changes it, or escalate", name)
		}
	}
	fields, _ := request.Report["fields"].(map[string]any)
	if fields == nil {
		fields = map[string]any{}
		request.Report["fields"] = fields
	}
	for _, name := range slices.Sorted(maps.Keys(changed)) {
		reported, ok := fields[name]
		if !ok {
			fields[name] = changed[name]
			continue
		}
		same, err := sameDraftValue(reported, changed[name])
		if err != nil {
			return err
		}
		if !same && name == fieldBody {
			return fmt.Errorf("accept reports body, which the proposal rewrites: edit the passages its hunks leave alone with %s, or contest the finding whose edit changes it", fieldPatch)
		}
		if !same {
			return fmt.Errorf("accept reports %s otherwise than the proposal writes it: leave %s out to take the proposal's, or contest the finding whose edit changes it", name, name)
		}
	}
	if _, rewritten := changed[fieldBody]; !rewritten {
		return nil
	}
	if patch, ok := fields[fieldPatch]; ok {
		body, err := mergeDeepBodyPatch(inst.Store, patch)
		if err != nil {
			return fmt.Errorf("field %s: %w: contest the finding to change its hunk", fieldPatch, err)
		}
		fields[fieldBody] = body
		delete(fields, fieldPatch)
	}
	return nil
}

// mergeDeepBodyPatch applies a reported bodyPatch over the body of the
// proposal the store's exchange built.
func mergeDeepBodyPatch(store *engine.Store, patch any) (string, error) {
	raw, err := json.Marshal(patch)
	if err != nil {
		return "", err
	}
	var edits []model.DeepEdit
	err = json.Unmarshal(raw, &edits)
	if err != nil {
		return "", fmt.Errorf("not a list of {old, new} pairs: %w", err)
	}
	changes, err := deepProposalChanges(store)
	if err != nil {
		return "", err
	}
	base, _ := workflowStoreString(store, fieldDeepBase)
	return model.MergeDeepBody(base, changes, edits)
}

// deepProposalChanges derives the proposal's changes from the exchange's
// findings and the verifier's responses.
func deepProposalChanges(store *engine.Store) ([]model.DeepChange, error) {
	findings, err := storeDeep[model.DeepFinding](store, fieldDeepFindings)
	if err != nil {
		return nil, err
	}
	history, err := storeDeep[model.DeepResponse](store, fieldDeepHistory)
	if err != nil {
		return nil, err
	}
	return model.DeepChanges(findings, history), nil
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
