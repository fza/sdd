package application

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/serveview"
)

// A person's one answer at an assisted session's approval gate may settle
// several sealed drafts (engine.Session.Sealed): the answer names the others
// under batch, each listed under the choice it takes. Every listed draft takes
// that answer as its own, with the same words, so each approval binds to its
// own draft; one the person excludes takes adjust and returns for revision.

type batchItem struct {
	instance, choice string
}

// readBatch validates the batch an answer of choice at chooser of primary
// carries before any of it is applied.
func (w *WorkflowSession) readBatch(primary, chooser string, raw any) ([]batchItem, error) {
	if raw == nil {
		return nil, nil
	}
	listed, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("batch maps each choice to the sealed drafts that take it")
	}
	if err := w.session.CheckSealed(primary, chooser); err != nil {
		return nil, fmt.Errorf("batch is answered only at an assisted session's approval gate: %w", err)
	}
	seen := map[string]bool{}
	var items []batchItem
	for _, choice := range slices.Sorted(maps.Keys(listed)) {
		ids, ok := listed[choice].([]any)
		if !ok {
			return nil, fmt.Errorf("batch lists the sealed drafts taking %q as instance ids", choice)
		}
		for _, raw := range ids {
			id, ok := raw.(string)
			if !ok {
				return nil, fmt.Errorf("batch lists each sealed draft as its instance id, got %v", raw)
			}
			switch {
			case id == primary:
				return nil, fmt.Errorf("batch names the draft it answers, %s: list only the others", id)
			case seen[id]:
				return nil, fmt.Errorf("batch names %s more than once", id)
			}
			seen[id] = true
			if err := w.session.CheckSealed(id, chooser); err != nil {
				return nil, fmt.Errorf("batch: %w", err)
			}
			inst, _ := w.session.Instance(id)
			option := answeredOption(inst.Spec, chooser, choice)
			if option == nil {
				return nil, fmt.Errorf("batch choice %q is not an option at %s", choice, chooser)
			}
			for _, field := range option.Collect {
				if !field.Optional {
					return nil, fmt.Errorf("batch choice %q requires field %q, which a batch does not carry: answer %s on its own", choice, field.Name, id)
				}
			}
			items = append(items, batchItem{instance: id, choice: choice})
		}
	}
	return items, nil
}

// answerBatch gives each listed draft the person's answer and returns the
// line naming where each stands.
func (w *WorkflowSession) answerBatch(ctx context.Context, identity RequestIdentity, chooser, words string, items []batchItem) (string, error) {
	settled := make([]string, 0, len(items))
	for _, item := range items {
		err := w.applyStagedBody(ctx, identity, WorkflowAdvanceRequest{Instance: item.instance, Report: map[string]any{"chooser": chooser, "choice": item.choice}}, true)
		var serve *engine.Serve
		if err == nil {
			serve, err = w.session.AnswerWith(item.instance, chooser, item.choice, nil, engine.AnswerWords{User: words})
		}
		if err == nil {
			err = w.session.SinkErr()
		}
		if err != nil {
			return "", fmt.Errorf("batch stopped at %s (%s), after settling [%s]: %w", item.instance, item.choice, strings.Join(settled, "; "), err)
		}
		at := "now at " + serve.Step
		if serve.Status != engine.StatusRunning {
			at = "ended (" + serve.Outcome + ")"
		}
		settled = append(settled, fmt.Sprintf("%s (%s) %s", item.instance, item.choice, at))
	}
	return "The same answer settled: " + strings.Join(settled, "; ") + ". Call next on each to carry it on.", nil
}

// sealedDraftsText lists the drafts other than instance that wait at their
// approval gate, each with the first line of its body.
func (w *WorkflowSession) sealedDraftsText(instance string) string {
	var lines []string
	for _, id := range w.session.Sealed() {
		if id == instance {
			continue
		}
		inst, _ := w.session.Instance(id)
		kind, _ := workflowStoreString(inst.Store, "entryKind")
		body, _ := workflowStoreString(inst.Store, "body")
		first, _, _ := strings.Cut(strings.TrimSpace(body), "\n")
		lines = append(lines, fmt.Sprintf("- %s: %s, %s", id, kind, model.ShortTitle(first, 100)))
	}
	return strings.Join(lines, "\n")
}

func (w *WorkflowSession) registerBatch(registry *engine.Registry) error {
	return registry.RegisterQuery(engine.Query{
		Doc: engine.FuncDoc{
			Name: "sealedDrafts",
			Doc:  "The other drafts of the session waiting at their approval gate in an assisted dialogue (engine.Session.Sealed), one line each: one answer of the person may settle them together.",
		},
		ServeSafe: true,
		Bound:     engine.QueryBound{Part: serveview.PartText, Cap: serveview.Default().Cap(serveview.PartText)},
		Fn: func(ctx *engine.Context, _ map[string]any) (any, error) {
			return w.sealedDraftsText(ctx.Instance), nil
		},
	})
}
