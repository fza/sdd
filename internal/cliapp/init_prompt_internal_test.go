package cliapp

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// The init question runs only on a terminal, so its options are checked here:
// every dialogue mode is offered, each with a hint.
func TestInitOffersEveryDialogueMode(t *testing.T) {
	options := dialogueModeOptions()
	if len(options) != len(model.DialogueModes()) {
		t.Fatalf("the question should offer %v, got %d options", model.DialogueModes(), len(options))
	}
	for i, mode := range model.DialogueModes() {
		if options[i].Value != mode || options[i].Hint == "" {
			t.Errorf("option %d should offer %q with a hint, got %+v", i, mode, options[i])
		}
	}
}
