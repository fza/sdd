package application

import (
	"strings"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

const (
	fieldReasonWords = "reasonWords"
	fieldIntentWords = "intentWords"
)

// userWordsMissing lists the fields holding the user's own words that the
// draft still owes; an autonomous session owes none.
func (w *WorkflowSession) userWordsMissing(store *engine.Store) []string {
	if w.Autonomous() {
		return nil
	}
	kind, _ := workflowStoreString(store, "entryKind")
	reason, intent := model.UserWordsOwed(model.Kind(kind))
	var missing []string
	if words, _ := workflowStoreString(store, fieldReasonWords); reason && strings.TrimSpace(words) == "" {
		missing = append(missing, fieldReasonWords)
	}
	if words, _ := workflowStoreString(store, fieldIntentWords); intent && strings.TrimSpace(words) == "" {
		missing = append(missing, fieldIntentWords)
	}
	return missing
}

func (w *WorkflowSession) registerUserWords(registry *engine.Registry) error {
	return registry.RegisterPredicate(engine.Predicate{
		Doc: engine.FuncDoc{
			Name: "userWordsHeld",
			Doc: "A decision drafted with a user present carries the user's own words for its reason, and a directive's for its intent " +
				"(model.UserWordsOwed); an autonomous session and a signal owe none.",
			Reads: []string{"entryKind", fieldReasonWords, fieldIntentWords},
		},
		Fn: func(ctx *engine.Context) (bool, error) {
			return len(w.userWordsMissing(ctx.Store)) == 0, nil
		},
		FailMessage: "the draft lacks the user's own words for the decision's reason or intent",
		FailDetail: func(ctx *engine.Context) string {
			missing := w.userWordsMissing(ctx.Store)
			if len(missing) == 0 {
				return ""
			}
			return "missing " + strings.Join(missing, ", ") + ": ask the user for the decision's reason" +
				" (and a directive's intent), end your turn, and after the answer report their words verbatim; never supply them"
		},
	})
}
