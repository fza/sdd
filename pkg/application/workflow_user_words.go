package application

import (
	"fmt"

	"github.com/networkteam/sdd/internal/model"
)

const (
	fieldReasonWords = "reasonWords"
	fieldIntentWords = "intentWords"
)

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
