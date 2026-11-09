package engine_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

const briefLoopMachine = `state:
    brief: {type: text, desc: the answer the command judges}
steps:
    - id: ask
      chooser: agent
      options:
          - {choice: send, collect: [brief], call: checkBrief, to: confirm}
    - id: confirm
      chooser: agent
      options:
          - {choice: again, to: ask}
          - {choice: finish, to: end(completed)}`

func briefLoopSession(t *testing.T) (*engine.Engine, *engine.Spec, *engine.Session, *recordingSink, string) {
	t.Helper()
	registry := engine.NewRegistry()
	err := registry.RegisterCommand(engine.Command{
		Doc: engine.FuncDoc{Name: "checkBrief", Doc: "Refuses a brief that answers no finding.", Reads: []string{"brief"}},
		Fn: func(ctx *engine.Context) error {
			brief, _ := ctx.Store.Get("brief")
			if text, _ := brief.(string); !strings.HasPrefix(text, "a1") {
				return errors.New("the brief answers no finding")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	spec, err := engine.LoadSpec(delegateEntry(t, "briefloop", "move", briefLoopMachine), registry)
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.New(registry, engine.StaticGraphs{Graph: model.NewGraph(nil)})
	sink := &recordingSink{}
	sess := eng.NewSession("s_brief", "tester", sink)
	sv, err := sess.Start(spec, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	return eng, spec, sess, sink, sv.Instance
}

// A command recording a mutation intent keeps a failed answer: its retry
// runs the same answer to the end.
func TestFailedIntentAnswerStaysForItsRetry(t *testing.T) {
	registry := engine.NewRegistry()
	failures := 1
	err := registry.RegisterCommand(engine.Command{
		Doc:              engine.FuncDoc{Name: "checkBrief", Doc: "Writes the brief once its first attempt failed.", Reads: []string{"brief"}},
		GraphIndependent: true,
		Prepare:          func(*engine.Context) (map[string]string, error) { return map[string]string{"entryId": "stable"}, nil },
		Fn: func(ctx *engine.Context) error {
			if failures > 0 {
				failures--
				return errors.New("the write failed")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	spec, err := engine.LoadSpec(delegateEntry(t, "briefloop", "move", briefLoopMachine), registry)
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.New(registry, engine.StaticGraphs{Graph: model.NewGraph(nil)})
	sink := &recordingSink{}
	sess := eng.NewSession("s_brief", "tester", sink)
	sv, err := sess.Start(spec, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sess.Answer(sv.Instance, "ask", "send", map[string]any{"brief": "a1: kept"}, "")
	var opErr *engine.OperationError
	if !errors.As(err, &opErr) {
		t.Fatalf("the failed command should leave a retryable operation, got %v", err)
	}
	inst, _ := sess.Instance(sv.Instance)
	if brief, _ := inst.Store.Get("brief"); brief != "a1: kept" {
		t.Fatalf("a failed answer with an intent should keep its fields for the retry, got %v", brief)
	}
	if _, err := sess.Retry(sv.Instance, opErr.Intent.Ref); err != nil {
		t.Fatalf("retry: %v", err)
	}
	replayed, err := eng.ReplaySession("s_brief", "tester", sink.events, func(string) (*engine.Spec, error) { return spec, nil }, nopSink{})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	again, _ := replayed.Instance(sv.Instance)
	if !reflect.DeepEqual(again.Store.Export(), inst.Store.Export()) || again.Step != inst.Step || inst.Step != "confirm" {
		t.Fatalf("replay should restore the retried answer, got %+v at %s, live %+v at %s", again.Store.Export(), again.Step, inst.Store.Export(), inst.Step)
	}
}

func TestRefusedAnswerLeavesTheInstanceAsItWas(t *testing.T) {
	eng, spec, sess, sink, instance := briefLoopSession(t)
	if _, err := sess.Answer(instance, "ask", "send", map[string]any{"brief": "a1: first"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Answer(instance, "confirm", "again", nil, ""); err != nil {
		t.Fatal(err)
	}
	logged := len(sink.events)

	if _, err := sess.Answer(instance, "ask", "send", map[string]any{"brief": "nothing"}, ""); err == nil {
		t.Fatal("the command should refuse the answer")
	}
	inst, _ := sess.Instance(instance)
	if brief, _ := inst.Store.Get("brief"); brief != "a1: first" || inst.Step != "ask" {
		t.Fatalf("a refused answer should leave the store and step as they were, got %v at %s", brief, inst.Step)
	}
	if len(sink.events) != logged {
		t.Fatalf("a refused answer should log nothing, logged %+v", sink.events[logged:])
	}

	if _, err := sess.Answer(instance, "ask", "send", map[string]any{"brief": "a1: second"}, ""); err != nil {
		t.Fatalf("an accepted answer: %v", err)
	}
	tail := sink.events[logged:]
	if len(tail) < 2 || tail[0].Event != engine.EventChooserAnswer || tail[1].Event != engine.EventOpResult {
		t.Fatalf("an accepted answer should log its answer right before its command's result, got %+v", tail)
	}

	replayed, err := eng.ReplaySession("s_brief", "tester", sink.events, func(string) (*engine.Spec, error) { return spec, nil }, nopSink{})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	again, _ := replayed.Instance(instance)
	if !reflect.DeepEqual(again.Store.Export(), inst.Store.Export()) || again.Step != inst.Step {
		t.Fatalf("replay should restore the live position, got %+v at %s, live %+v at %s", again.Store.Export(), again.Step, inst.Store.Export(), inst.Step)
	}
}
