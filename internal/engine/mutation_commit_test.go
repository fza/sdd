package engine_test

import (
	"errors"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

// An operation that commits an effect and then fails keeps the commit: its
// retry, live or after a replay, continues past the effect instead of
// repeating it, and the outcome carries the committed field.
func TestRetryContinuesPastACommittedEffect(t *testing.T) {
	for _, tc := range []struct {
		name   string
		replay bool
	}{
		{name: "live retry"},
		{name: "retry after replay", replay: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := engine.NewRegistry()
			effects, failures := 0, 1
			err := registry.RegisterCommand(engine.Command{
				Doc:              engine.FuncDoc{Name: "checkBrief", Doc: "Sends the brief, then fails once behind the send.", Reads: []string{"brief"}, Writes: []string{"receipt"}},
				GraphIndependent: true,
				Prepare:          func(*engine.Context) (map[string]string, error) { return map[string]string{"receipt": "r1"}, nil },
				Fn: func(ctx *engine.Context) error {
					if _, sent := ctx.Committed["receipt"]; !sent {
						effects++
						if err := ctx.Commit(map[string]any{"receipt": ctx.Intent.Values["receipt"]}); err != nil {
							return err
						}
					}
					if failures > 0 {
						failures--
						return errors.New("the step behind the send failed")
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
			_, err = sess.Answer(sv.Instance, "ask", "send", map[string]any{"brief": "a1"}, "")
			var opErr *engine.OperationError
			if !errors.As(err, &opErr) {
				t.Fatalf("the failure behind the send should leave a retryable operation, got %v", err)
			}
			resolve := func(string) (*engine.Spec, error) { return spec, nil }
			if tc.replay {
				sess, err = eng.ReplaySession("s_brief", "tester", sink.events, resolve, sink)
				if err != nil {
					t.Fatalf("replay: %v", err)
				}
			}
			if _, err := sess.Retry(sv.Instance, opErr.Intent.Ref); err != nil {
				t.Fatalf("retry: %v", err)
			}
			if effects != 1 {
				t.Fatalf("the retry should continue past the committed send, it sent %d times", effects)
			}
			inst, _ := sess.Instance(sv.Instance)
			if receipt, _ := inst.Store.Get("receipt"); receipt != "r1" || inst.Step != "confirm" {
				t.Fatalf("the finished operation should hold the committed field, got %v at %s", receipt, inst.Step)
			}
			replayed, err := eng.ReplaySession("s_brief", "tester", sink.events, resolve, nopSink{})
			if err != nil {
				t.Fatalf("replay of the finished operation: %v", err)
			}
			again, _ := replayed.Instance(sv.Instance)
			if receipt, _ := again.Store.Get("receipt"); receipt != "r1" {
				t.Fatalf("replay should restore the committed field, got %v", receipt)
			}
		})
	}
}
