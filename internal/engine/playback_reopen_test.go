package engine_test

import (
	"testing"
)

// confirmMachine reaches check without a confirmation while the fast
// selector holds, as an autonomous capture reaches pre-flight before
// playback.
const confirmMachine = `state:
    body: {type: text, desc: x}
steps:
    - id: draft
      collect: [body]
      transitions:
          - when: hasBody and fast
            to: check
          - when: hasBody
            to: playback
    - id: playback
      chooser: user
      options:
          - {choice: confirm, call: confirmPlayback, to: check}
    - id: check
      guard: playbackConfirmed or fast
      transitions:
          - when: ready
            to: end(completed)`

func TestAGateOwingANeverRecordedConfirmationReopensItsChooser(t *testing.T) {
	w := newGateWorld(t)
	w.values["fast"] = true
	session := w.engine.NewSession("s_confirm", "tester", &memorySink{})
	serve, err := session.Start(mustParseLanes(t, confirmMachine, "## unit: draft\n\nDraft.\n\n## unit: playback\n\nPlay back.\n"), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	instance := serve.Instance
	serve, err = session.Report(instance, map[string]any{"body": "a draft"})
	if err != nil {
		t.Fatal(err)
	}
	if serve.Step != "check" {
		t.Fatalf("fast should carry the draft to check unconfirmed, got %s", serve.Step)
	}

	w.values["fast"] = false
	w.values["ready"] = true
	serve, err = session.Poll(instance)
	if err != nil {
		t.Fatal(err)
	}
	if serve.Step != "playback" || serve.Chooser == nil {
		t.Fatalf("a gate owing a confirmation no one recorded should reopen the chooser that records it, got step %s diagnostics %v", serve.Step, serve.Diagnostics)
	}

	serve, err = session.Answer(instance, "playback", "confirm", nil, "yes, write it")
	if err != nil {
		t.Fatal(err)
	}
	if serve.Status != "completed" {
		t.Fatalf("the confirmed draft should pass the gate, got step %s status %s diagnostics %v", serve.Step, serve.Status, serve.Diagnostics)
	}
}
