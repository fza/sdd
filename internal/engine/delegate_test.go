package engine_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

// The dispatcher and its review task exchange rounds across two sessions: the
// task delivers plan as findings (received as synthesis), waits for the
// dispatcher's brief (sent as contract), and delivers body as its final result
// (received as briefing). Field names come from the presence vocabulary only
// where a guard reads them.
const dispatcherMachine = `state:
    goal: {type: text, desc: what the review is for}
    synthesis: {type: text, desc: the delegate's findings}
    briefing: {type: text, desc: the delegate's final result}
    brief: {type: text, desc: the answers to the findings}
steps:
    - id: ask
      chooser: agent
      options:
          - {choice: delegate, collect: [goal], dispatch: {procedure: reviewtask, seed: {topic: goal}, receive: {synthesis: plan, briefing: body}, send: {contract: brief}}, to: wait}
    - id: wait
      await: [synthesis]
      transitions:
          - otherwise: answer
    - id: answer
      chooser: agent
      options:
          - {choice: reply, collect: [brief], to: wait}
          - {choice: close, collect: [brief], to: settle}
    - id: settle
      await: [briefing]
      transitions:
          - otherwise: end(completed)`

const reviewTaskMachine = `rounds: 2
state:
    topic: {type: text, desc: what to review}
    plan: {type: text, desc: the findings}
    contract: {type: text, desc: the dispatching side's answers}
    body: {type: text, desc: the final result}
steps:
    - id: review
      collect: [plan]
      deliver: [plan]
      transitions:
          - when: hasPlan
            to: discuss
    - id: discuss
      await: [contract]
      transitions:
          - otherwise: respond
    - id: respond
      chooser: agent
      options:
          - {choice: again, collect: [plan], to: review}
          - {choice: done, collect: [body], to: finish}
    - id: finish
      deliver: [body]
      transitions:
          - otherwise: end(completed)`

func delegateEntry(t *testing.T, canonical, class, machine string) *model.Entry {
	t.Helper()
	content := "---\ntype: decision\nlayer: prc\nkind: procedure\ncanonical: " + canonical + "\nclass: " + class + "\n" +
		machine + "\n---\n\nA synthetic delegate exchange.\n"
	entry, err := model.ParseEntry("20261109-120000-d-prc-dlg.md", content)
	if err != nil {
		t.Fatalf("fixture entry: %v", err)
	}
	return entry
}

func delegateSpec(t *testing.T, canonical, class, machine string) *engine.Spec {
	t.Helper()
	spec, err := engine.LoadSpec(delegateEntry(t, canonical, class, machine), engine.NewRegistry())
	if err != nil {
		t.Fatalf("LoadSpec %s: %v", canonical, err)
	}
	return spec
}

// sessionCourier reaches sessions held in memory by ID, the way a shell
// reaches stored ones.
type sessionCourier struct {
	sessions map[string]*engine.Session
	fail     error
	carried  []engine.Delivery
}

func (c *sessionCourier) Deliver(d engine.Delivery) (string, error) {
	if c.fail != nil {
		return "", c.fail
	}
	target, ok := c.sessions[d.To.Session]
	if !ok {
		return "", errors.New("unreachable session")
	}
	c.carried = append(c.carried, d)
	return target.Receive(d)
}

type exchangeFixture struct {
	courier     *sessionCourier
	parent      *engine.Session
	parentSink  *recordingSink
	child       *engine.Session
	childSink   *recordingSink
	parentInst  string
	childInst   string
	childSpec   *engine.Spec
	parentSpec  *engine.Spec
	childServes *engine.Serve
}

// newExchange answers the dispatcher's delegate option, forks the review task
// into its own session and starts it there.
func newExchange(t *testing.T) *exchangeFixture {
	t.Helper()
	f := &exchangeFixture{
		courier:    &sessionCourier{sessions: map[string]*engine.Session{}},
		parentSink: &recordingSink{},
		childSink:  &recordingSink{},
		parentSpec: delegateSpec(t, "dispatcher", "move", dispatcherMachine),
		childSpec:  delegateSpec(t, "reviewtask", "task", reviewTaskMachine),
	}
	eng := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)})
	f.parent = eng.NewSession("s_parent", "tester", f.parentSink, engine.WithCourier(f.courier))
	f.courier.sessions["s_parent"] = f.parent
	sv, err := f.parent.Start(f.parentSpec, nil, "")
	if err != nil {
		t.Fatalf("Start dispatcher: %v", err)
	}
	f.parentInst = sv.Instance
	if _, err := f.parent.Answer(f.parentInst, "ask", "delegate", map[string]any{"goal": "check the draft"}, ""); err != nil {
		t.Fatalf("answer delegate: %v", err)
	}
	f.child = eng.NewSession("s_child", "tester", f.childSink,
		engine.WithLineage(engine.Link{Session: "s_parent", Instance: f.parentInst}), engine.WithCourier(f.courier))
	f.courier.sessions["s_child"] = f.child
	f.childInst = f.child.NextInstanceID()
	seed, err := f.parent.Fork(f.parentInst, engine.Link{Session: "s_child", Instance: f.childInst}, f.childSpec)
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	if seed["topic"] != "check the draft" {
		t.Fatalf("the fork should seed the child from the parent's goal, got %v", seed)
	}
	f.childServes, err = f.child.Start(f.childSpec, seed, "")
	if err != nil {
		t.Fatalf("Start review task: %v", err)
	}
	if f.childServes.Instance != f.childInst {
		t.Fatalf("the child started as %s, but the fork recorded %s", f.childServes.Instance, f.childInst)
	}
	return f
}

func (f *exchangeFixture) parentStep(t *testing.T) string {
	t.Helper()
	inst, _ := f.parent.Instance(f.parentInst)
	return inst.Step
}

func (f *exchangeFixture) childInstance(t *testing.T) *engine.Instance {
	t.Helper()
	inst, _ := f.child.Instance(f.childInst)
	return inst
}

func laneText(sv *engine.Serve, name string) string {
	for _, lane := range sv.Lanes {
		if lane.Name == name {
			return lane.Text
		}
	}
	return ""
}

func TestInterimDeliveryLandsWithoutCompletingTheChildOrMovingTheParent(t *testing.T) {
	f := newExchange(t)
	if got := f.parentStep(t); got != "wait" {
		t.Fatalf("the dispatcher should wait for the delegate, at %s", got)
	}
	sv, err := f.child.Report(f.childInst, map[string]any{"plan": "f1: the scope is unclear"})
	if err != nil {
		t.Fatalf("child report: %v", err)
	}
	if sv.Status != engine.StatusRunning || sv.Step != "discuss" {
		t.Fatalf("an interim delivery leaves the child running at its await gate, got %s at %s", sv.Status, sv.Step)
	}
	if got := f.parentStep(t); got != "wait" {
		t.Fatalf("a delivery must not move the dispatcher's step, now at %s", got)
	}
	parent, _ := f.parent.Instance(f.parentInst)
	if v, _ := parent.Store.Get("synthesis"); v != "f1: the scope is unclear" {
		t.Fatalf("the findings should land in the dispatcher's receive field, got %v", v)
	}
	if len(f.parent.LiveForks()) != 1 {
		t.Fatalf("an interim delivery keeps the fork live, got %+v", f.parent.LiveForks())
	}
	picked, err := f.parent.Poll(f.parentInst)
	if err != nil {
		t.Fatalf("parent poll: %v", err)
	}
	if picked.Step != "answer" {
		t.Fatalf("the pickup call should pass the await gate, at %s", picked.Step)
	}
}

func TestParentFilledGateServesTheChildsNextStepOnceFilled(t *testing.T) {
	f := newExchange(t)
	if _, err := f.child.Report(f.childInst, map[string]any{"plan": "f1"}); err != nil {
		t.Fatal(err)
	}
	held, err := f.child.Poll(f.childInst)
	if err != nil {
		t.Fatal(err)
	}
	if held.Step != "discuss" || !slices.ContainsFunc(held.Diagnostics, func(d string) bool {
		return strings.Contains(d, "waiting for the dispatching move to send contract")
	}) {
		t.Fatalf("the child should hold at its await gate naming what it waits for, got %s %v", held.Step, held.Diagnostics)
	}
	if _, err := f.parent.Poll(f.parentInst); err != nil {
		t.Fatal(err)
	}
	if _, err := f.parent.Answer(f.parentInst, "answer", "close", map[string]any{"brief": "a1: scope is the capture step"}, ""); err != nil {
		t.Fatalf("parent answer: %v", err)
	}
	if got := f.childInstance(t).Step; got != "discuss" {
		t.Fatalf("the fill must not move the child's step before it picks it up, at %s", got)
	}
	next, err := f.child.Poll(f.childInst)
	if err != nil {
		t.Fatal(err)
	}
	if next.Step != "respond" {
		t.Fatalf("the filled gate should serve the child's next step, at %s", next.Step)
	}
	exchange := laneText(next, "exchange")
	if !strings.Contains(exchange, "You delivered:\n- plan: f1") || !strings.Contains(exchange, "The dispatching move sent:\n- contract: a1: scope is the capture step") {
		t.Fatalf("the child's serve should carry the full exchange so far, got %q", exchange)
	}
}

func TestChildCompletesOnlyAtItsFinalDelivery(t *testing.T) {
	f := newExchange(t)
	if _, err := f.child.Report(f.childInst, map[string]any{"plan": "f1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.parent.Poll(f.parentInst); err != nil {
		t.Fatal(err)
	}
	if _, err := f.parent.Answer(f.parentInst, "answer", "close", map[string]any{"brief": "a1"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.child.Poll(f.childInst); err != nil {
		t.Fatal(err)
	}
	if got := f.childInstance(t).Status; got != engine.StatusRunning {
		t.Fatalf("the child stays running until its final delivery, got %s", got)
	}
	done, err := f.child.Answer(f.childInst, "respond", "done", map[string]any{"body": "settled: one finding stands"}, "")
	if err != nil {
		t.Fatalf("child final: %v", err)
	}
	if done.Status != engine.StatusCompleted {
		t.Fatalf("the final delivery completes the child, got %s", done.Status)
	}
	if forks := f.parent.LiveForks(); len(forks) != 0 {
		t.Fatalf("a final delivery ends the fork as completed, still live: %+v", forks)
	}
	settled, err := f.parent.Poll(f.parentInst)
	if err != nil {
		t.Fatal(err)
	}
	if settled.Status != engine.StatusCompleted {
		t.Fatalf("the dispatcher should pick up the final result and complete, got %s at %s", settled.Status, settled.Step)
	}
}

func TestChildConcludesAtItsRoundCap(t *testing.T) {
	f := newExchange(t)
	if _, err := f.child.Report(f.childInst, map[string]any{"plan": "f1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.parent.Poll(f.parentInst); err != nil {
		t.Fatal(err)
	}
	if _, err := f.parent.Answer(f.parentInst, "answer", "reply", map[string]any{"brief": "a1"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.child.Poll(f.childInst); err != nil {
		t.Fatal(err)
	}
	capped, err := f.child.Answer(f.childInst, "respond", "again", map[string]any{"plan": "f2"}, "")
	if err != nil {
		t.Fatalf("second round: %v", err)
	}
	if capped.Status != engine.StatusAbandoned {
		t.Fatalf("the second interim delivery reaches the round cap of 2, so the child concludes, got %s at %s", capped.Status, capped.Step)
	}
	last := f.childSink.events[len(f.childSink.events)-1]
	if last.Event != engine.EventAbandoned || last.Data["reason"] != "round cap 2 reached" {
		t.Fatalf("the conclusion should name the round cap, last event %+v", last)
	}
	picked, err := f.parent.Poll(f.parentInst)
	if err != nil {
		t.Fatal(err)
	}
	if picked.Step != "answer" {
		t.Fatalf("the capping round's delivery still lands, dispatcher at %s", picked.Step)
	}
	refused, err := f.parent.Answer(f.parentInst, "answer", "reply", map[string]any{"brief": "a2"}, "")
	if err != nil {
		t.Fatalf("a fill to a concluded child is a notice, not an error: %v", err)
	}
	if notice := laneText(refused, "delivery"); !strings.Contains(notice, "contract") || !strings.Contains(notice, "has ended") {
		t.Fatalf("the refused fill should stay as a named notice, got %q", notice)
	}
	if forks := f.parent.LiveForks(); len(forks) != 0 {
		t.Fatalf("a refused fill ends the fork, still live: %+v", forks)
	}
}

func TestEndedParentConcludesTheChildOnItsNextDelivery(t *testing.T) {
	f := newExchange(t)
	if err := f.parent.Abandon(f.parentInst, "user stopped"); err != nil {
		t.Fatal(err)
	}
	sv, err := f.child.Report(f.childInst, map[string]any{"plan": "f1"})
	if err != nil {
		t.Fatalf("an undeliverable delivery is a refusal, not an error: %v", err)
	}
	if sv.Status != engine.StatusAbandoned {
		t.Fatalf("a delegate whose dispatching move ended concludes, got %s", sv.Status)
	}
	last := f.childSink.events[len(f.childSink.events)-1]
	if reason, _ := last.Data["reason"].(string); !strings.HasPrefix(reason, "undeliverable: ") {
		t.Fatalf("the conclusion should name the undeliverable delivery, got %+v", last)
	}
}

func TestCourierFailureLeavesTheSenderWhereItWas(t *testing.T) {
	f := newExchange(t)
	f.courier.fail = errors.New("store unavailable")
	if _, err := f.child.Report(f.childInst, map[string]any{"plan": "f1"}); err == nil || !strings.Contains(err.Error(), "store unavailable") {
		t.Fatalf("a failed delivery should fail the operation loudly, got %v", err)
	}
	if got := f.childInstance(t).Step; got != "review" {
		t.Fatalf("a failed delivery must not move the sender, at %s", got)
	}
	f.courier.fail = nil
	sv, err := f.child.Poll(f.childInst)
	if err != nil {
		t.Fatal(err)
	}
	if sv.Step != "discuss" {
		t.Fatalf("the next pickup should retry the delivery, at %s", sv.Step)
	}
}

func TestRepeatedDeliveryLandsOnce(t *testing.T) {
	f := newExchange(t)
	if _, err := f.child.Report(f.childInst, map[string]any{"plan": "f1"}); err != nil {
		t.Fatal(err)
	}
	repeat := f.courier.carried[len(f.courier.carried)-1]
	if refusal, err := f.parent.Receive(repeat); err != nil || refusal != "" {
		t.Fatalf("a repeated delivery is accepted as a no-op, got %q %v", refusal, err)
	}
	if n := len(f.parent.Exchange(f.parentInst)); n != 1 {
		t.Fatalf("a repeated delivery should land once, exchange holds %d", n)
	}
}

func TestDeliveryOnlyFieldsRefuseReports(t *testing.T) {
	f := newExchange(t)
	if _, err := f.child.Report(f.childInst, map[string]any{"contract": "forged answer"}); err == nil || !strings.Contains(err.Error(), "filled only by a delivery") {
		t.Fatalf("the delegate must not stand in for the dispatcher's fill, got %v", err)
	}
	if _, err := f.parent.Report(f.parentInst, map[string]any{"synthesis": "forged findings"}); err == nil || !strings.Contains(err.Error(), "filled only by a delivery") {
		t.Fatalf("the dispatcher must not stand in for the delegate's delivery, got %v", err)
	}
}

func TestReceiveRefusesUndeclaredFields(t *testing.T) {
	f := newExchange(t)
	fromChild := engine.Delivery{ID: "x/1", From: engine.Link{Session: "s_child", Instance: f.childInst}, To: engine.Link{Session: "s_parent", Instance: f.parentInst}, Fields: map[string]any{"topic": "x"}}
	if _, err := f.parent.Receive(fromChild); err == nil || !strings.Contains(err.Error(), "does not receive") {
		t.Fatalf("a delegate field the dispatch does not receive is an error, got %v", err)
	}
	fromParent := engine.Delivery{ID: "x/2", From: engine.Link{Session: "s_parent", Instance: f.parentInst}, To: engine.Link{Session: "s_child", Instance: f.childInst}, Fields: map[string]any{"plan": "x"}}
	if _, err := f.child.Receive(fromParent); err == nil || !strings.Contains(err.Error(), "does not await") {
		t.Fatalf("a field no child gate awaits is an error, got %v", err)
	}
	stranger := engine.Delivery{ID: "x/3", From: engine.Link{Session: "s_other", Instance: "i_1"}, To: engine.Link{Session: "s_parent", Instance: f.parentInst}, Fields: map[string]any{"plan": "x"}}
	if _, err := f.parent.Receive(stranger); err == nil || !strings.Contains(err.Error(), "forked no delegate") {
		t.Fatalf("a delivery from a session the dispatcher never forked is an error, got %v", err)
	}
}

func TestExchangeReplaysOnBothSides(t *testing.T) {
	f := newExchange(t)
	if _, err := f.child.Report(f.childInst, map[string]any{"plan": "f1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.parent.Poll(f.parentInst); err != nil {
		t.Fatal(err)
	}
	if _, err := f.parent.Answer(f.parentInst, "answer", "reply", map[string]any{"brief": "a1"}, ""); err != nil {
		t.Fatal(err)
	}
	resolve := func(canonical string) (*engine.Spec, error) {
		if canonical == "dispatcher" {
			return f.parentSpec, nil
		}
		return f.childSpec, nil
	}
	eng := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)})
	courier := &sessionCourier{sessions: map[string]*engine.Session{}}
	parent, err := eng.ReplaySession("s_parent", "tester", f.parentSink.events, resolve, nopSink{}, engine.WithCourier(courier))
	if err != nil {
		t.Fatalf("replay parent: %v", err)
	}
	child, err := eng.ReplaySession("s_child", "tester", f.childSink.events, resolve, nopSink{}, engine.WithCourier(courier))
	if err != nil {
		t.Fatalf("replay child: %v", err)
	}
	courier.sessions["s_parent"], courier.sessions["s_child"] = parent, child
	if !child.NonInteractive() || *child.Lineage != (engine.Link{Session: "s_parent", Instance: f.parentInst}) {
		t.Fatalf("replay should restore the child's lineage, got %+v", child.Lineage)
	}
	if forks := parent.LiveForks(); len(forks) != 1 || forks[0].Fork.Send["contract"] != "brief" {
		t.Fatalf("replay should restore the live fork and its mappings, got %+v", forks)
	}
	held, err := parent.Poll(f.parentInst)
	if err != nil {
		t.Fatal(err)
	}
	if held.Step != "wait" {
		t.Fatalf("a consumed delivery stays consumed after replay, dispatcher at %s", held.Step)
	}
	next, err := child.Poll(f.childInst)
	if err != nil {
		t.Fatal(err)
	}
	if next.Step != "respond" || !strings.Contains(laneText(next, "exchange"), "contract: a1") {
		t.Fatalf("an unconsumed fill survives replay with the exchange, got %s %q", next.Step, laneText(next, "exchange"))
	}
	capped, err := child.Answer(f.childInst, "respond", "again", map[string]any{"plan": "f2"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if capped.Status != engine.StatusAbandoned {
		t.Fatalf("replay should keep the round count toward the cap, got %s", capped.Status)
	}
}

func TestForkChecksTheMappingsCoverTheTask(t *testing.T) {
	cases := []struct {
		name     string
		dispatch string
		want     string
	}{
		{name: "delivered field not received", dispatch: `{procedure: reviewtask, receive: {synthesis: plan}, send: {contract: brief}}`, want: `delivers "body"`},
		{name: "awaited field never sent", dispatch: `{procedure: reviewtask, receive: {synthesis: plan, briefing: body}}`, want: `awaits "contract"`},
		{name: "sent field not awaited", dispatch: `{procedure: reviewtask, receive: {synthesis: plan, briefing: body}, send: {contract: brief, topic: brief}}`, want: `sends "topic"`},
		{name: "another procedure", dispatch: `{procedure: othertask, receive: {synthesis: plan, briefing: body}, send: {contract: brief}}`, want: "dispatches othertask"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			machine := strings.Replace(dispatcherMachine, `{procedure: reviewtask, seed: {topic: goal}, receive: {synthesis: plan, briefing: body}, send: {contract: brief}}`, tc.dispatch, 1)
			parentSpec := delegateSpec(t, "dispatcher", "move", machine)
			childSpec := delegateSpec(t, "reviewtask", "task", reviewTaskMachine)
			eng := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)})
			parent := eng.NewSession("s_parent", "tester", nopSink{})
			sv, err := parent.Start(parentSpec, nil, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parent.Answer(sv.Instance, "ask", "delegate", map[string]any{"goal": "g"}, ""); err != nil {
				t.Fatal(err)
			}
			_, err = parent.Fork(sv.Instance, engine.Link{Session: "s_child", Instance: "i_1"}, childSpec)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Fork = %v, want an error naming %s", err, tc.want)
			}
			if len(parent.LiveForks()) != 0 {
				t.Fatal("a refused fork records nothing")
			}
		})
	}
}

func TestDelegateSessionStartRules(t *testing.T) {
	childSpec := delegateSpec(t, "reviewtask", "task", reviewTaskMachine)
	parentSpec := delegateSpec(t, "dispatcher", "move", dispatcherMachine)
	eng := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)})

	plain := eng.NewSession("s_plain", "tester", nopSink{})
	if _, err := plain.Start(childSpec, map[string]any{"topic": "t"}, ""); err == nil || !strings.Contains(err.Error(), "runs only in a delegate session") {
		t.Fatalf("a delivering task outside a delegate session should be refused, got %v", err)
	}

	child := eng.NewSession("s_child", "tester", nopSink{}, engine.WithLineage(engine.Link{Session: "s_parent", Instance: "i_1"}))
	if _, err := child.Start(parentSpec, nil, ""); err == nil || !strings.Contains(err.Error(), "runs a task") {
		t.Fatalf("a delegate session runs a task only, got %v", err)
	}
	if _, err := child.Start(childSpec, map[string]any{"topic": "t"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := child.Start(childSpec, map[string]any{"topic": "t"}, ""); err == nil || !strings.Contains(err.Error(), "runs only the task it was forked for") {
		t.Fatalf("a delegate session runs exactly one task, got %v", err)
	}
}

func TestDelegateSpecLoadRules(t *testing.T) {
	cases := []struct {
		name, class, machine, want string
	}{
		{name: "deliver outside a task", class: "move", machine: `state:
    plan: {type: text, desc: x}
steps:
    - id: only
      collect: [plan]
      deliver: [plan]
      transitions:
          - otherwise: end(completed)`, want: "only a task procedure delivers"},
		{name: "await on a chooser", class: "move", machine: `state:
    plan: {type: text, desc: x}
steps:
    - id: only
      chooser: agent
      await: [plan]
      options:
          - {choice: go, to: end(completed)}`, want: "deliver and await apply to gate steps only"},
		{name: "awaited field collected", class: "move", machine: `state:
    plan: {type: text, desc: x}
steps:
    - id: only
      collect: [plan]
      await: [plan]
      transitions:
          - otherwise: end(completed)`, want: `collects "plan", which only a delivery fills`},
		{name: "received field collected", class: "move", machine: `state:
    plan: {type: text, desc: x}
steps:
    - id: only
      chooser: agent
      options:
          - {choice: go, collect: [plan], dispatch: {receive: {plan: findings}}, to: end(completed)}`, want: `collects "plan", which only a delivery fills`},
		{name: "interim delivery without a round cap", class: "task", machine: strings.Replace(reviewTaskMachine, "rounds: 2\n", "", 1), want: "declares its round cap"},
		{name: "round cap without an interim delivery", class: "task", machine: `rounds: 1
state:
    body: {type: text, desc: x}
steps:
    - id: only
      collect: [body]
      deliver: [body]
      transitions:
          - when: hasBody
            to: end(completed)`, want: "declared without an interim delivery"},
		{name: "empty dispatch", class: "move", machine: `steps:
    - id: only
      chooser: agent
      options:
          - {choice: go, dispatch: {procedure: x}, to: end(completed)}`, want: "needs a seed, receive or send mapping"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := engine.ParseSpec(delegateEntry(t, "rules", tc.class, tc.machine))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ParseSpec = %v, want a problem naming %q", err, tc.want)
			}
		})
	}
}

func TestEndedForkRefusesTheDelegatesDelivery(t *testing.T) {
	f := newExchange(t)
	if err := f.parent.EndFork(f.parentInst, engine.Link{Session: "s_child", Instance: f.childInst}, "released"); err != nil {
		t.Fatal(err)
	}
	sv, err := f.child.Report(f.childInst, map[string]any{"plan": "f1"})
	if err != nil {
		t.Fatalf("a delivery to an ended fork is a refusal, not an error: %v", err)
	}
	last := f.childSink.events[len(f.childSink.events)-1]
	if reason, _ := last.Data["reason"].(string); sv.Status != engine.StatusAbandoned || !strings.Contains(reason, "delegate fork of dispatcher has ended") {
		t.Fatalf("a delegate whose fork ended concludes naming it, got %s %+v", sv.Status, last)
	}
	parent, _ := f.parent.Instance(f.parentInst)
	if parent.Store.Has("synthesis") {
		t.Fatal("an ended fork's delivery must not land")
	}
}

// An option's command checks the answer before its send-mapped fields leave:
// a refused answer sends nothing, so a delegate never works an answer the
// dispatching move would not accept.
func TestRefusedOptionCommandWithholdsTheSend(t *testing.T) {
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
	machine := strings.Replace(dispatcherMachine, "{choice: reply, collect: [brief], to: wait}", "{choice: reply, collect: [brief], call: checkBrief, to: wait}", 1)
	parentSpec, err := engine.LoadSpec(delegateEntry(t, "dispatcher", "move", machine), registry)
	if err != nil {
		t.Fatal(err)
	}
	childSpec := delegateSpec(t, "reviewtask", "task", reviewTaskMachine)
	courier := &sessionCourier{sessions: map[string]*engine.Session{}}
	eng := engine.New(registry, engine.StaticGraphs{Graph: model.NewGraph(nil)})
	parent := eng.NewSession("s_parent", "tester", &recordingSink{}, engine.WithCourier(courier))
	courier.sessions["s_parent"] = parent
	sv, err := parent.Start(parentSpec, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parent.Answer(sv.Instance, "ask", "delegate", map[string]any{"goal": "check the draft"}, ""); err != nil {
		t.Fatal(err)
	}
	child := eng.NewSession("s_child", "tester", &recordingSink{}, engine.WithLineage(engine.Link{Session: "s_parent", Instance: sv.Instance}), engine.WithCourier(courier))
	courier.sessions["s_child"] = child
	childInst := child.NextInstanceID()
	seed, err := parent.Fork(sv.Instance, engine.Link{Session: "s_child", Instance: childInst}, childSpec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := child.Start(childSpec, seed, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := child.Report(childInst, map[string]any{"plan": "f1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := parent.Poll(sv.Instance); err != nil {
		t.Fatal(err)
	}
	_, err = parent.Answer(sv.Instance, "answer", "reply", map[string]any{"brief": "nothing"}, "")
	if err == nil {
		t.Fatal("the command should refuse the answer")
	}
	if len(courier.carried) != 1 {
		t.Fatalf("a refused answer should send nothing, carried %d deliveries", len(courier.carried))
	}
	if _, err := parent.Answer(sv.Instance, "answer", "reply", map[string]any{"brief": "a1: the capture step"}, ""); err != nil {
		t.Fatalf("an accepted answer: %v", err)
	}
	next, err := child.Poll(childInst)
	if err != nil {
		t.Fatal(err)
	}
	inst, _ := child.Instance(childInst)
	if contract, _ := inst.Store.Get("contract"); next.Step != "respond" || contract != "a1: the capture step" {
		t.Fatalf("the accepted answer should reach the child, child at %s holding %v", next.Step, contract)
	}
}

func TestForksListEndedForksBesideLiveOnes(t *testing.T) {
	f := newExchange(t)
	if forks := f.parent.Forks(f.parentInst); len(forks) != 1 || forks[0].Status != engine.ForkLive || forks[0].Procedure != "reviewtask" {
		t.Fatalf("the dispatching instance should list its live fork, got %+v", forks)
	}
	if err := f.parent.EndFork(f.parentInst, engine.Link{Session: "s_child", Instance: f.childInst}, "replaced"); err != nil {
		t.Fatal(err)
	}
	if forks := f.parent.Forks(f.parentInst); len(forks) != 1 || forks[0].Status != engine.ForkConcluded || len(f.parent.LiveForks()) != 0 {
		t.Fatalf("an ended fork should stay listed, no longer live, got %+v", forks)
	}
}
