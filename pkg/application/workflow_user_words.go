package application

import (
	"fmt"
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

// reasonRepeatsIntent reports whether the draft's reason only restates the
// decision its intent words state.
func (w *WorkflowSession) reasonRepeatsIntent(store *engine.Store) bool {
	if w.Autonomous() {
		return false
	}
	reason, _ := workflowStoreString(store, fieldReasonWords)
	intent, _ := workflowStoreString(store, fieldIntentWords)
	return model.ReasonRepeatsIntent(reason, intent)
}

func (w *WorkflowSession) registerUserWords(registry *engine.Registry) error {
	return registry.RegisterPredicate(engine.Predicate{
		Doc: engine.FuncDoc{
			Name: "userWordsHeld",
			Doc: "A decision drafted with a user present carries the user's own words for its reason, and a directive's for its intent " +
				"(model.UserWordsOwed), and a reason that only restates the intent (model.ReasonRepeatsIntent) counts as none; " +
				"an autonomous session and a signal owe none.",
			Reads: []string{"entryKind", fieldReasonWords, fieldIntentWords},
		},
		Fn: func(ctx *engine.Context) (bool, error) {
			return len(w.userWordsMissing(ctx.Store)) == 0 && !w.reasonRepeatsIntent(ctx.Store), nil
		},
		FailMessage: "the draft lacks the user's own words for the decision's reason or intent",
		FailDetail: func(ctx *engine.Context) string {
			missing := w.userWordsMissing(ctx.Store)
			if len(missing) == 0 {
				if w.reasonRepeatsIntent(ctx.Store) {
					return "reasonWords repeats intentWords, the decision itself: ask the user why they decided it, end your turn," +
						" and after the answer report their words verbatim; never supply them"
				}
				return ""
			}
			return "missing " + strings.Join(missing, ", ") + ": ask the user for the decision's reason" +
				" (and a directive's intent), end your turn, and after the answer report their words verbatim; never supply them"
		},
	})
}

// checkUserWordsQuoted refuses an answer relaying the user's message that sets
// the user's quoted words to anything that message does not say: a wording
// change to the draft goes into the body, never into these quotes.
func (w *WorkflowSession) checkUserWordsQuoted(instanceID string, fields map[string]any, userWords string) error {
	inst, ok := w.session.Instance(instanceID)
	if !ok || userWords == "" {
		return nil
	}
	for _, field := range []string{fieldReasonWords, fieldIntentWords} {
		words, _ := fields[field].(string)
		held, _ := workflowStoreString(inst.Store, field)
		if words == "" || words == held || model.QuotedFrom(words, userWords) {
			continue
		}
		return fmt.Errorf("%s must quote the user's words in this answer as they said them, and these do not: a wording change to the draft goes into the body, never into %s; report %s only with words the user gave for it", field, field, field)
	}
	return nil
}
