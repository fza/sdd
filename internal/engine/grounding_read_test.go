package engine_test

import (
	"bytes"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

const groundingMachine = `state:
    note: {type: text, desc: anything}
steps:
    - id: draft
      chooser: agent
      options:
          - {choice: finish, to: end(completed)}`

func groundingSession(t *testing.T) (*engine.Engine, *engine.Spec, *bytes.Buffer, *engine.Session, string) {
	t.Helper()
	spec := historySpec(t, groundingMachine)
	eng := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)})
	var log bytes.Buffer
	session := eng.NewSession("s_grounding", "tester", engine.NewWriterSink(&log))
	sv, err := session.Start(spec, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	return eng, spec, &log, session, sv.Instance
}

func groundingPayload() model.ReadPayload {
	return model.ReadPayload{
		Reads: []model.GroundingRead{
			{Mode: model.GroundingSemantic, Text: "grounding before capture", IDs: []string{"a", "b"}},
			{Mode: model.GroundingKeyword, Text: "gather", IDs: []string{"b", "c"}},
			{Mode: model.GroundingListing, Text: "reliability/grounding", IDs: []string{"d"}},
		},
		Full:   []string{"b"},
		Corpus: model.NewCorpusMark([]string{"a", "b", "c", "d"}),
	}
}

func TestGroundingRead_ReplayRestoresEveryLoggedRead(t *testing.T) {
	eng, spec, log, session, instance := groundingSession(t)
	if err := session.Record(engine.Read{Tool: "gather", Instance: instance, Full: []string{"b"}, Summary: []string{"a", "c", "d"}, Grounding: groundingPayload()}); err != nil {
		t.Fatal(err)
	}
	if err := session.Record(engine.Read{Tool: "gather", Instance: instance, Full: []string{"c"}, Grounding: model.ReadPayload{
		Reads: []model.GroundingRead{{Mode: model.GroundingOpened, IDs: []string{"c"}}}, Full: []string{"c"},
		Corpus: model.NewCorpusMark([]string{"later"}),
	}}); err != nil {
		t.Fatal(err)
	}
	session.LogRead("search", nil, []string{"e"})

	events, err := engine.ReadEvents(log)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := eng.ReplaySession("s_grounding", "tester", events, func(string) (*engine.Spec, error) { return spec, nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	live, _ := session.Instance(instance)
	restored, ok := replayed.Instance(instance)
	if !ok {
		t.Fatal("replay lost the instance")
	}
	want, got := live.Reading(), restored.Reading()
	if !slices.Equal(got.Listed(), want.Listed()) || !slices.Equal(got.Full(), want.Full()) {
		t.Fatalf("replayed reading listed %v full %v, want listed %v full %v", got.Listed(), got.Full(), want.Listed(), want.Full())
	}
	if len(got.Reads) != 4 || got.Reads[0].Text != "grounding before capture" || got.Reads[3].Mode != model.GroundingOpened {
		t.Fatalf("replayed reads = %+v, want the four reads as run", got.Reads)
	}
	if got.Corpus == nil || !slices.Equal(got.Corpus.IDs, []string{"a", "b", "c", "d"}) {
		t.Fatalf("replayed corpus mark = %+v, want the first read's mark", got.Corpus)
	}
	for id, depth := range map[string]engine.ReadDepth{"b": engine.ReadFull, "c": engine.ReadFull, "a": engine.ReadSummary, "e": engine.ReadSummary} {
		if replayed.ReadDepthOf(id) != depth {
			t.Errorf("replayed depth of %s = %q, want %q", id, replayed.ReadDepthOf(id), depth)
		}
	}
}

func TestGroundingRead_ReadsOutsideTheInstanceCreditNothing(t *testing.T) {
	_, spec, _, session, instance := groundingSession(t)
	session.LogRead("show", []string{"a"}, []string{"b"})
	live, _ := session.Instance(instance)
	if listed := live.Reading().Listed(); len(listed) != 0 {
		t.Fatalf("a session-level read credited the instance: %v", listed)
	}
	if err := session.Record(engine.Read{Tool: "gather", Grounding: groundingPayload()}); err == nil {
		t.Fatal("a grounding read naming no instance was accepted")
	}

	// A read made before an instance starts cannot credit it.
	sv, err := session.Start(spec, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	later, _ := session.Instance(sv.Instance)
	if len(later.Reading().Listed()) != 0 {
		t.Fatalf("an instance started after a read holds credit: %v", later.Reading().Listed())
	}

	if _, err := session.Answer(instance, "draft", "finish", nil, ""); err != nil {
		t.Fatal(err)
	}
	if err := session.Record(engine.Read{Tool: "gather", Instance: instance, Grounding: groundingPayload()}); err == nil {
		t.Fatal("a read credited an ended instance")
	}
	if listed := live.Reading().Listed(); len(listed) != 0 {
		t.Fatalf("the ended instance took credit: %v", listed)
	}
}

func TestGroundingRead_EmptyPayloadAppendsNothing(t *testing.T) {
	_, _, log, session, instance := groundingSession(t)
	before := log.Len()
	if err := session.Record(engine.Read{Tool: "gather", Instance: instance}); err != nil {
		t.Fatal(err)
	}
	session.LogRead("view", nil, nil)
	if log.Len() != before {
		t.Fatalf("an empty read appended %d bytes to the log", log.Len()-before)
	}
}
