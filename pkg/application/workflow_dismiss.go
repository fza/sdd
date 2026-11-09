package application

import (
	"context"
	"fmt"

	"github.com/networkteam/sdd/internal/engine"
)

// WorkflowDismissRequest sets entries a running capture read aside with a
// verdict code (20261109-004506-d-cpt-udz, D9).
type WorkflowDismissRequest struct {
	Instance string
	IDs      []string
	Code     string
}

// Dismiss adds one verdict to the capture's verdicts, keeping those it holds,
// and reports them at the capture's current step, as an agent reporting
// verdicts would.
func (w *WorkflowSession) Dismiss(ctx context.Context, identity RequestIdentity, request WorkflowDismissRequest) (*WorkflowServe, error) {
	w.setOperation(ctx, identity)
	inst, ok := w.session.Instance(request.Instance)
	if !ok {
		return nil, fmt.Errorf("instance %q not found in session", request.Instance)
	}
	if inst.Status != engine.StatusRunning {
		return nil, fmt.Errorf("instance %s has ended (%s): only a running capture takes verdicts", request.Instance, inst.Outcome)
	}
	var verdicts []any
	for _, doc := range workflowStoreDocuments(inst.Store, "verdicts") {
		verdicts = append(verdicts, doc)
	}
	ids := make([]any, len(request.IDs))
	for i, id := range request.IDs {
		ids[i] = id
	}
	verdicts = append(verdicts, map[string]any{"ids": ids, "code": request.Code})
	return w.Advance(ctx, identity, WorkflowAdvanceRequest{Instance: request.Instance, Report: map[string]any{"verdicts": verdicts}})
}
