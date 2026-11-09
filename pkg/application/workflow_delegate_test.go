package application_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdd "github.com/networkteam/sdd/pkg/application"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

// The dispatcher forks the review task into a delegate child session and
// exchanges rounds with it: the task delivers plan (received as synthesis),
// waits for the dispatcher's brief (sent as contract), and delivers body as
// its final result (received as briefing).
const delegateDispatcherProcedure = `---
type: decision
kind: procedure
layer: process
canonical: dispatcher
confidence: high
summary: A move dispatching a review delegate for the delegate tests.
state:
    goal: {type: text, desc: what the review is for}
    synthesis: {type: text, desc: the delegate's findings}
    briefing: {type: text, desc: the delegate's final result}
    brief: {type: text, desc: the answers to the findings}
steps:
    - id: ask
      chooser: agent
      options:
          - {choice: delegate, collect: [goal], dispatch: {procedure: reviewtask, seed: {topic: goal}, receive: {synthesis: plan, briefing: body}, send: {contract: brief}}, to: wait}
          - {choice: echo, collect: [goal], dispatch: {procedure: echotask, seed: {goal: goal}, receive: {briefing: goal}}, to: settle}
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
          - otherwise: end(completed)
---

Dispatches a review delegate.
`

const delegateTaskProcedure = `---
type: decision
kind: procedure
layer: process
canonical: reviewtask
class: task
rounds: 2
confidence: high
summary: A review task exchanging rounds with its dispatcher for the delegate tests.
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
          - otherwise: end(completed)
---

Reviews {{.topic}} in rounds.

## unit: review

Review {{.topic}} and report plan.
`

// The echo task delivers its seed at once, inside the call that forks it.
const delegateEchoProcedure = `---
type: decision
kind: procedure
layer: process
canonical: echotask
class: task
confidence: high
summary: A task delivering its seed as soon as it starts, for the delegate tests.
state:
    goal: {type: text, desc: what to echo}
steps:
    - id: echo
      deliver: [goal]
      transitions:
          - when: hasGoal
            to: end(completed)
---

Echoes its goal.
`

type delegateFixture struct {
	app      *sdd.Application
	sessions sdd.SessionStore
	identity sdd.RequestIdentity
	parent   sdd.SessionID
	dispatch string
}

func newDelegateApplication(t *testing.T, launcher sdd.DelegateLauncher) *delegateFixture {
	t.Helper()
	graphDir := t.TempDir()
	for name, content := range map[string]string{
		"13-060000-d-prc-dsp.md": delegateDispatcherProcedure,
		"13-060100-d-prc-rvt.md": delegateTaskProcedure,
		"13-060200-d-prc-ech.md": delegateEchoProcedure,
	} {
		path := filepath.Join(graphDir, "2026", "07", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	graph, err := localadapter.NewFilesystemGraphStore(localadapter.FilesystemGraphStoreOptions{Project: "example", GraphDir: graphDir})
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := localadapter.NewFilesystemSessionStoreAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := localadapter.NewFilesystemStagedBlobStoreAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := sdd.NewProjectRuntime(sdd.ProjectRuntimeOptions{
		Project: sdd.ProjectRef{ID: "example"}, Graph: graph,
		LLM: pkgllm.RunnerFunc(func(context.Context, pkgllm.Request) (pkgllm.Result, error) {
			return pkgllm.Result{Identity: pkgllm.Identity{Provider: "test", Model: "test"}}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	application, err := sdd.NewApplication(sdd.ApplicationOptions{
		Access: &failAtPrincipalResolver{runtime: runtime}, Sessions: sessions, StagedBlobs: blobs, DelegateLauncher: launcher,
	})
	if err != nil {
		t.Fatal(err)
	}
	f := &delegateFixture{app: application, sessions: sessions, identity: sdd.RequestIdentity{Subject: "christopher"}}
	workflow, _, err := application.OpenWorkflow(t.Context(), f.identity, "example", sdd.WorkflowOpenRequest{ClientName: "test"})
	if err != nil {
		t.Fatal(err)
	}
	f.parent = workflow.ID()
	serve, err := workflow.Start(t.Context(), f.identity, sdd.WorkflowStartRequest{Canonical: "dispatcher"})
	if err != nil {
		t.Fatal(err)
	}
	f.dispatch = serve.Instance
	answer := map[string]any{"chooser": "ask", "choice": "delegate", "fields": map[string]any{"goal": "the capture draft"}}
	if _, err := workflow.Advance(t.Context(), f.identity, sdd.WorkflowAdvanceRequest{Instance: f.dispatch, Report: answer}); err != nil {
		t.Fatal(err)
	}
	return f
}

// session replays a session afresh, as every MCP call does.
func (f *delegateFixture) session(t *testing.T, id sdd.SessionID) *sdd.WorkflowSession {
	t.Helper()
	workflow, err := f.app.RefreshWorkflow(t.Context(), f.identity, id)
	if err != nil {
		t.Fatalf("refresh %s: %v", id, err)
	}
	return workflow
}

func (f *delegateFixture) advance(t *testing.T, id sdd.SessionID, instance string, report map[string]any) *sdd.WorkflowServe {
	t.Helper()
	serve, err := f.session(t, id).Advance(t.Context(), f.identity, sdd.WorkflowAdvanceRequest{Instance: instance, Report: report})
	if err != nil {
		t.Fatalf("advance %s %s: %v", id, instance, err)
	}
	return serve
}

func (f *delegateFixture) fork(t *testing.T) (*sdd.WorkflowServe, *sdd.WorkflowDispatch) {
	t.Helper()
	serve, err := f.session(t, f.parent).Start(t.Context(), f.identity, sdd.WorkflowStartRequest{Canonical: "reviewtask", Parent: f.dispatch})
	if err != nil {
		t.Fatalf("dispatching the review task: %v", err)
	}
	if len(serve.Dispatches) != 1 {
		t.Fatalf("a fork should owe exactly one dispatch handoff, got %+v", serve.Dispatches)
	}
	return serve, serve.Dispatches[0]
}

func (f *delegateFixture) ended(t *testing.T, id sdd.SessionID) *sdd.SessionEnd {
	t.Helper()
	stored, err := f.sessions.Load(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return stored.Metadata.Ended
}

func TestDispatchingADeliveringTaskForksADelegateSession(t *testing.T) {
	f := newDelegateApplication(t, nil)
	serve, dispatch := f.fork(t)
	if serve.Session != f.parent || serve.Instance != f.dispatch || serve.Step != "wait" {
		t.Fatalf("the dispatching move should be served where it waits, got %s %s at %s", serve.Session, serve.Instance, serve.Step)
	}
	if dispatch.Session == f.parent || dispatch.Session == "" {
		t.Fatalf("the delegate should run in its own child session, got %q", dispatch.Session)
	}
	if !strings.Contains(dispatch.Instructions, "fresh-context sub-agent") {
		t.Fatalf("the first round should name the fresh-context launch, got %q", dispatch.Instructions)
	}
	for _, prompt := range []string{dispatch.Prompt, dispatch.ContinuePrompt} {
		if !strings.Contains(prompt, string(dispatch.Session)) || strings.Contains(prompt, string(f.parent)) {
			t.Fatalf("a delegate prompt carries the child handle and never the dispatching one, got %q", prompt)
		}
	}
	child, resume, err := f.app.ResumeWorkflow(t.Context(), f.identity, sdd.WorkflowResumeRequest{SessionID: dispatch.Session})
	if err != nil {
		t.Fatalf("the delegate resumes its child session by handle: %v", err)
	}
	if len(resume.Open) != 1 || resume.Open[0].Procedure != "reviewtask" || resume.Open[0].Step != "review" {
		t.Fatalf("the child session should hold the task alone, got %+v", resume.Open)
	}
	if !strings.Contains(resume.Open[0].Instructions, "Review the capture draft") {
		t.Fatalf("the child should start with the dispatch's seed, got %q", resume.Open[0].Instructions)
	}
	if child.Branch() != "" || child.Project() != "example" {
		t.Fatalf("the child inherits the dispatching session's branch binding and project, got %q %q", child.Branch(), child.Project())
	}
	if _, err := child.Start(t.Context(), f.identity, sdd.WorkflowStartRequest{Canonical: "dispatcher"}); err == nil {
		t.Fatal("a delegate session should refuse every move beside its task")
	}
}

func TestDelegateRoundsCrossSessions(t *testing.T) {
	f := newDelegateApplication(t, nil)
	_, dispatch := f.fork(t)
	child := dispatch.Session

	delivered := f.advance(t, child, dispatch.Instance, map[string]any{"plan": "f1: the scope is unclear"})
	if delivered.Status != "running" || delivered.Step != "discuss" {
		t.Fatalf("the interim delivery leaves the delegate waiting, got %s at %s", delivered.Status, delivered.Step)
	}
	picked := f.advance(t, f.parent, f.dispatch, nil)
	if picked.Step != "answer" {
		t.Fatalf("a report-less next picks the delivery up, dispatcher at %s", picked.Step)
	}

	sent := f.advance(t, f.parent, f.dispatch, map[string]any{"chooser": "answer", "choice": "close", "fields": map[string]any{"brief": "a1: the capture step"}})
	if len(sent.Dispatches) != 1 || sent.Dispatches[0].Session != child {
		t.Fatalf("sending the delegate its next input owes a continue-or-relaunch handoff, got %+v", sent.Dispatches)
	}
	if next := sent.Dispatches[0]; !strings.Contains(next.Instructions, "continue mechanism") || !strings.Contains(next.Instructions, "fresh-context") || strings.Contains(next.Prompt+next.ContinuePrompt, string(f.parent)) {
		t.Fatalf("the later-round handoff names continue and relaunch without the dispatching handle, got %+v", next)
	}

	_, resume, err := f.app.ResumeWorkflow(t.Context(), f.identity, sdd.WorkflowResumeRequest{SessionID: child})
	if err != nil {
		t.Fatal(err)
	}
	if exchange := resume.Open[0].Instructions; !strings.Contains(exchange, "- plan: f1: the scope is unclear") || !strings.Contains(exchange, "- contract: a1: the capture step") {
		t.Fatalf("a relaunched delegate's serve carries the full exchange, got %q", exchange)
	}
	responded := f.advance(t, child, dispatch.Instance, nil)
	if responded.Step != "respond" {
		t.Fatalf("the delegate picks the fill up with a report-less next, at %s", responded.Step)
	}
	done := f.advance(t, child, dispatch.Instance, map[string]any{"chooser": "respond", "choice": "done", "fields": map[string]any{"body": "settled"}})
	if done.Status != "completed" || !strings.Contains(done.Instructions, "Stop here") {
		t.Fatalf("the final delivery completes the delegate and tells it to stop, got %s %q", done.Status, done.Instructions)
	}
	if f.ended(t, child) == nil {
		t.Fatal("a completed delegate's session ends with its task")
	}
	settled := f.advance(t, f.parent, f.dispatch, nil)
	if settled.Status != "completed" {
		t.Fatalf("the dispatcher picks the final result up, got %s at %s", settled.Status, settled.Step)
	}
}

func TestReportlessNextWritesNothingWhileNothingLanded(t *testing.T) {
	f := newDelegateApplication(t, nil)
	f.fork(t)
	before, err := f.sessions.Load(t.Context(), f.parent)
	if err != nil {
		t.Fatal(err)
	}
	held := f.advance(t, f.parent, f.dispatch, nil)
	if held.Step != "wait" || !strings.Contains(strings.Join(held.Diagnostics, "\n"), "waiting for the delegate to deliver synthesis") {
		t.Fatalf("the pickup re-serves the waiting position, got %s %v", held.Step, held.Diagnostics)
	}
	after, err := f.sessions.Load(t.Context(), f.parent)
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != before.Version {
		t.Fatalf("a clean poll should append nothing, version %d became %d", before.Version, after.Version)
	}
}

func TestDelegateLivesNoLongerThanItsDispatchingMove(t *testing.T) {
	t.Run("abandoned move", func(t *testing.T) {
		f := newDelegateApplication(t, nil)
		_, dispatch := f.fork(t)
		if _, err := f.session(t, f.parent).Abandon(t.Context(), f.identity, f.dispatch, "not needed"); err != nil {
			t.Fatal(err)
		}
		end := f.ended(t, dispatch.Session)
		if end == nil || !strings.Contains(end.Reason, "dispatching move dispatcher ended (abandoned)") {
			t.Fatalf("abandoning the dispatching move abandons its delegate, got %+v", end)
		}
	})
	t.Run("torn-down session", func(t *testing.T) {
		f := newDelegateApplication(t, nil)
		_, dispatch := f.fork(t)
		if _, err := f.app.AbandonWorkflowSession(t.Context(), f.identity, sdd.WorkflowResumeRequest{SessionID: f.parent}, "done for today"); err != nil {
			t.Fatal(err)
		}
		if end := f.ended(t, dispatch.Session); end == nil || !strings.Contains(end.Reason, "dispatching session was abandoned") {
			t.Fatalf("tearing the dispatching session down ends its delegate, got %+v", end)
		}
	})
	t.Run("torn-down delegate", func(t *testing.T) {
		f := newDelegateApplication(t, nil)
		_, dispatch := f.fork(t)
		f.advance(t, dispatch.Session, dispatch.Instance, map[string]any{"plan": "f1"})
		if _, err := f.app.AbandonWorkflowSession(t.Context(), f.identity, sdd.WorkflowResumeRequest{SessionID: dispatch.Session}, "stuck"); err != nil {
			t.Fatal(err)
		}
		f.advance(t, f.parent, f.dispatch, nil)
		sent := f.advance(t, f.parent, f.dispatch, map[string]any{"chooser": "answer", "choice": "reply", "fields": map[string]any{"brief": "a1"}})
		if len(sent.Dispatches) != 0 || strings.Contains(sent.Instructions, "was not delivered") {
			t.Fatalf("a delegate torn down by handle releases its fork, so nothing is even attempted, got %+v %q", sent.Dispatches, sent.Instructions)
		}
	})
}

// scriptedLauncher records every launch and drives the first round's
// delegate through the application, the way a headless CLI would through its
// own sdd server.
type scriptedLauncher struct {
	t        *testing.T
	fixture  *delegateFixture
	launches []sdd.DelegateLaunch
	fail     error
}

func (l *scriptedLauncher) LaunchDelegate(ctx context.Context, launch sdd.DelegateLaunch) (sdd.DelegateLaunched, error) {
	l.launches = append(l.launches, launch)
	if l.fail != nil {
		return sdd.DelegateLaunched{}, l.fail
	}
	if launch.Round == 1 {
		l.fixture.advance(l.t, launch.Session, launch.Instance, map[string]any{"plan": "f1 from the launched delegate"})
	}
	return sdd.DelegateLaunched{Session: "cli-" + string(rune('0'+launch.Round))}, nil
}

func TestLauncherKeepsItsSessionPerChildAndResumesEachRound(t *testing.T) {
	launcher := &scriptedLauncher{t: t}
	f := newDelegateApplication(t, launcher)
	launcher.fixture = f
	_, dispatch := f.fork(t)
	if dispatch.Launched != "cli-1" || !strings.Contains(dispatch.Instructions, "launch nothing") {
		t.Fatalf("a launched round says sdd launched it, got %+v", dispatch)
	}
	picked := f.advance(t, f.parent, f.dispatch, nil)
	if picked.Step != "answer" {
		t.Fatalf("the launched delegate's delivery lands in the dispatching move, at %s", picked.Step)
	}
	sent := f.advance(t, f.parent, f.dispatch, map[string]any{"chooser": "answer", "choice": "reply", "fields": map[string]any{"brief": "a1"}})
	if len(sent.Dispatches) != 1 || sent.Dispatches[0].Launched != "cli-2" {
		t.Fatalf("sending the next input launches the next round, got %+v", sent.Dispatches)
	}
	if len(launcher.launches) != 2 {
		t.Fatalf("one launch per round, got %d", len(launcher.launches))
	}
	first, second := launcher.launches[0], launcher.launches[1]
	if first.Round != 1 || first.Resume != "" || second.Round != 2 || second.Resume != "cli-1" {
		t.Fatalf("each round resumes the launcher session of the one before, got %+v then %+v", first, second)
	}
	if first.Session != dispatch.Session || second.Session != dispatch.Session {
		t.Fatalf("every launch names the child session, got %s and %s", first.Session, second.Session)
	}
	for _, launch := range launcher.launches {
		if strings.Contains(launch.Prompt, string(f.parent)) || !strings.Contains(launch.Prompt, string(dispatch.Session)) {
			t.Fatalf("a launched prompt carries the child handle and never the dispatching one, got %q", launch.Prompt)
		}
	}
	kept, err := f.app.DelegateLaunches(t.Context(), f.identity, dispatch.Session)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(kept, ",") != "cli-1,cli-2" {
		t.Fatalf("the launcher sessions are kept with the child session, got %v", kept)
	}
}

func TestLauncherFailureFailsTheDispatchLoudly(t *testing.T) {
	launcher := &scriptedLauncher{t: t, fail: errors.New("agent CLI not found")}
	f := newDelegateApplication(t, launcher)
	launcher.fixture = f
	_, err := f.session(t, f.parent).Start(t.Context(), f.identity, sdd.WorkflowStartRequest{Canonical: "reviewtask", Parent: f.dispatch})
	if err == nil || !strings.Contains(err.Error(), "agent CLI not found") {
		t.Fatalf("a failed launch should surface, got %v", err)
	}
}

func TestImmediateDeliveryReachesTheHeldDispatcher(t *testing.T) {
	f := newDelegateApplication(t, nil)
	workflow := f.session(t, f.parent)
	move, err := workflow.Start(t.Context(), f.identity, sdd.WorkflowStartRequest{Canonical: "dispatcher"})
	if err != nil {
		t.Fatal(err)
	}
	answer := map[string]any{"chooser": "ask", "choice": "echo", "fields": map[string]any{"goal": "echo me"}}
	if _, err := workflow.Advance(t.Context(), f.identity, sdd.WorkflowAdvanceRequest{Instance: move.Instance, Report: answer}); err != nil {
		t.Fatal(err)
	}
	forked, err := workflow.Start(t.Context(), f.identity, sdd.WorkflowStartRequest{Canonical: "echotask", Parent: move.Instance})
	if err != nil {
		t.Fatalf("dispatching the echo task: %v", err)
	}
	if strings.Contains(strings.Join(forked.Diagnostics, "\n"), "waiting for the delegate") {
		t.Fatalf("a delivery made while forking should land in the dispatching session the call holds, got %v", forked.Diagnostics)
	}
	if err := workflow.RecordServed(t.Context(), f.identity, []string{"served-after-fork"}); err != nil {
		t.Fatalf("the held dispatching session should stay writable after the fork: %v", err)
	}
	if f.ended(t, forked.Dispatches[0].Session) == nil {
		t.Fatal("the echo delegate completes inside the fork")
	}
}

func TestDelegateStartsInTheDispatchersProject(t *testing.T) {
	f := newDelegateApplication(t, nil)
	_, err := f.session(t, f.parent).Start(t.Context(), f.identity, sdd.WorkflowStartRequest{Canonical: "reviewtask", Parent: f.dispatch, Project: "elsewhere"})
	if err == nil || !strings.Contains(err.Error(), "a delegate works in the project of the move that dispatches it") {
		t.Fatalf("a delegate pinned to another project should be refused, got %v", err)
	}
}
