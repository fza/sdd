package engine_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

const seededMachine = `state:
    widenReport: {type: prose, seed: reading, desc: reads}
    verdicts: {type: list<verdict>, optional: true, confirmExempt: true, desc: set aside}
    body: {type: text, desc: content}
steps:
    - id: draft
      chooser: agent
      options:
          - {choice: finish, to: end(completed)}`

// A credited read seeds the declared field, keeping the text around the seed,
// and replay restores the seeded value (20261109-004506-d-cpt-udz, D32).
func TestGroundingSeed_ReadSeedsTheFieldAndReplayRestoresIt(t *testing.T) {
	spec := historySpec(t, seededMachine)
	eng := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)})
	var log bytes.Buffer
	session := eng.NewSession("s_seed", "tester", engine.NewWriterSink(&log))
	sv, err := session.Start(spec, map[string]any{"widenReport": "inherited from the dispatcher"}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range []model.ReadPayload{groundingPayload(), {Reads: []model.GroundingRead{{Mode: model.GroundingOpened, IDs: []string{"c"}}}, Full: []string{"c"}}} {
		err := session.Record(engine.Read{Tool: "gather", Instance: sv.Instance, Grounding: payload})
		if err != nil {
			t.Fatal(err)
		}
	}
	live, _ := session.Instance(sv.Instance)
	value, _ := live.Store.Get("widenReport")
	text, _ := value.(string)
	if !strings.HasPrefix(text, "inherited from the dispatcher\n\nReading recorded by sdd gather:") {
		t.Fatalf("the seed should follow the inherited text:\n%s", text)
	}
	if strings.Count(text, "Reading recorded by sdd gather:") != 1 || !strings.Contains(text, "keyword(gather): b, c") || !strings.Contains(text, "served in full: b, c") {
		t.Fatalf("the seed should be replaced in place with every read as run:\n%s", text)
	}

	events, err := engine.ReadEvents(&log)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := eng.ReplaySession("s_seed", "tester", events, func(string) (*engine.Spec, error) { return spec, nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	restored, _ := replayed.Instance(sv.Instance)
	again, _ := restored.Store.Get("widenReport")
	if again != text {
		t.Fatalf("replay restored a different seed:\n%v", again)
	}
}

// A confirmExempt field stays out of the confirmation snapshot.
func TestGroundingSeed_ConfirmExemptFieldLeavesTheSnapshot(t *testing.T) {
	spec := historySpec(t, seededMachine)
	store := engine.NewStore(spec)
	_, err := store.WriteState(map[string]any{"body": "the draft"})
	if err != nil {
		t.Fatal(err)
	}
	before := store.StateSnapshot()
	_, err = store.WriteState(map[string]any{"verdicts": []any{map[string]any{"ids": []any{"20260101-100000-s-tac-aaa"}, "code": model.VerdictCovered}}})
	if err != nil {
		t.Fatal(err)
	}
	if store.StateSnapshot() != before {
		t.Fatal("a confirmExempt field changed the confirmation snapshot")
	}
	_, err = store.WriteState(map[string]any{"body": "another draft"})
	if err != nil {
		t.Fatal(err)
	}
	if store.StateSnapshot() == before {
		t.Fatal("a content field left the confirmation snapshot unchanged")
	}
}

func TestGroundingSeed_SpecRefusesMisplacedAttributes(t *testing.T) {
	for name, machine := range map[string]string{
		"seed on a list":         "state:\n    reads: {type: list<text>, seed: reading, desc: x}\n",
		"unknown seed":           "state:\n    reads: {type: text, seed: verdicts, desc: x}\n",
		"confirmExempt on param": "params:\n    reads: {type: text, optional: true, confirmExempt: true, desc: x}\n",
	} {
		t.Run(name, func(t *testing.T) {
			content := "---\ntype: decision\nlayer: prc\nkind: procedure\ncanonical: seedproc\n" + machine +
				"steps:\n    - id: draft\n      chooser: agent\n      options:\n          - {choice: finish, to: end(completed)}\n---\n\nseed procedure\n"
			entry, err := model.ParseEntry("20260923-120000-d-prc-sed.md", content)
			if err != nil {
				t.Fatalf("fixture entry: %v", err)
			}
			_, err = engine.ParseSpec(entry)
			if err == nil {
				t.Fatal("the spec should be refused")
			}
		})
	}
}
