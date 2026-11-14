package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

// A task that delivers runs in a delegate child session forked on dispatch
// (20260827-232430-d-cpt-voa), exchanges rounds with the instance that
// dispatched it (20261109-004845-d-cpt-ldx), and is driven by a delegate the
// harness or a configured launcher starts and keeps working across rounds
// (20261109-004924-d-cpt-cxw). The child holds only its own handles; the
// dispatching session's handle never enters a delegate prompt.

const workflowDelegateLaunchCode = "workflow_delegate_launch"

// DelegateLauncher starts or resumes the agent that drives a delegate child
// session, for a composition where sdd itself launches delegates instead of
// the harness. It is called once the dispatching operation's own writes are
// done: on the fork, and each time the dispatching move sends the delegate its
// next input. It answers at once, taking the launch on or leaving it to the
// harness. sdd runs the round it took on in a process of its own
// (RoundProcesses), which asks the launcher again for the round to run, so the
// dispatching call never waits for a round: that move's next call picks the
// delegate's deliveries up, and takes a failed round to the waiting gate's
// delegateEnded route, or else fails with the round's error.
type DelegateLauncher interface {
	LaunchDelegate(ctx context.Context, launch DelegateLaunch) (DelegateRun, error)
}

// DelegateLaunch is the launch a delegate takes: the child handles and the
// project's launch. It carries nothing of the dispatching session.
type DelegateLaunch struct {
	Session   SessionID
	Instance  string
	Procedure string
	// Project is the project the delegate works in.
	Project ProjectID
	// Runner is the runner the project configures for this delegate: claude,
	// codex or driver, or DelegateLaunchFallback where the harness reported
	// no subagent tool; empty leaves the runner to the composition. A round's
	// process asks with the runner the launch reported.
	Runner string
}

// DelegateRun is a launch a launcher took on: the runner that runs, and Round,
// which runs one round to its end and stops once its context ends, as sdd ends
// it past verify.round_timeout. sdd runs one round of a child at a time. A round that ends while its delegate neither delivered
// nor concluded counts as failed.
type DelegateRun struct {
	Runner string
	Round  func(ctx context.Context, round DelegateRound) (DelegateRoundEnd, error)
}

// RoundProcesses runs each round sdd launches in a process of its own, which
// outlives the server that launched it and records the round's outcome itself
// (20261114-160323-d-tac-n0n). Any process of the composition answers for a
// round's process later: whether it still runs, and stopping it.
type RoundProcesses interface {
	// Log is the file the process of round writes its output and its log
	// to, which a failure of the round names.
	Log(round LaunchedRound) string
	// Start starts the process that runs round through RunLaunchedRound, in a
	// process group of its own, and returns it once it runs.
	Start(ctx context.Context, round LaunchedRound) (RoundProcess, error)
	// Alive reports whether process still runs: no process with its PID, or
	// one that started at another time, is gone.
	Alive(ctx context.Context, process RoundProcess) (bool, error)
	// Stop ends a running process with its whole process group, the agent CLI
	// it runs included. A process already gone leaves its group to agent, the
	// agent CLI it recorded, zero when none: Stop ends the group only while
	// agent still runs in it.
	Stop(ctx context.Context, process, agent RoundProcess) error
}

// RoundProcess is a round's process: its PID and its start stamp, which tells
// it from a later process that took the same PID.
type RoundProcess struct {
	PID     int
	Started string
}

// LaunchedRound is a round sdd launched, as its process runs it: the child
// handles, the project, the runner the launch reported and the round's number
// from 1.
type LaunchedRound struct {
	Session   SessionID
	Instance  string
	Procedure string
	Project   ProjectID
	Runner    string
	Number    int
}

// DelegateRoundEnd is what a round that ended reports: the launcher's own
// session for the next round to resume, and the agent's last message, which
// names why a round that delivered nothing failed.
type DelegateRoundEnd struct {
	Session string
	Message string
}

// DelegateRound is one round's input: its number from 1, its prompt, and the
// session the launcher reported for the previous round, empty on the first.
type DelegateRound struct {
	Number int
	Prompt string
	Resume string
	// AgentStarted, when set, records the agent CLI or driver the round
	// started, once it runs; the round stops with its error.
	AgentStarted func(ctx context.Context, agent RoundProcess) error
}

// DelegateLaunchFallback asks the launcher for the first headless runner of
// the agents the project's supported_agents name whose CLI is found
// (20261109-004729-d-cpt-xs5, V-D20).
const DelegateLaunchFallback = "fallback"

// ErrDelegateNotLaunched is what a launcher returns for a delegate it leaves
// to the harness: the dispatch then serves the harness's launch instructions.
var ErrDelegateNotLaunched = errors.New("the delegate is left to the harness to launch")

// WorkflowDispatch is the dispatch handoff served to the dispatching move: the
// child handles, the prompts a harness launches the delegate with, and the
// instructions for this round (20260829-122157-d-cpt-zpe). A delegate sdd
// launched itself is driven by sdd alone: its handoff carries the procedure,
// the runner and the instructions, and no handle or prompt the dispatching
// agent could work it with.
type WorkflowDispatch struct {
	Session   SessionID
	Instance  string
	Procedure string
	// Prompt starts a fresh-context delegate; ContinuePrompt continues one that
	// already worked an earlier round.
	Prompt         string
	ContinuePrompt string
	Instructions   string
	// Agent is the subagent definition a harness launches the delegate as.
	Agent string
	// Model and Effort are what the harness launches that subagent with, from
	// verify.claude or verify.codex as the MCP client tells; empty leaves the
	// harness's own.
	Model  string
	Effort string
	// Runner is the runner sdd runs the delegate's round with, when it launched
	// the delegate itself.
	Runner string
}

// delegateLaunchRecord is one launched round's outcome, kept with the child
// session: the launcher's session to resume, or the error the round failed
// with, and when it ended.
type delegateLaunchRecord struct {
	Round   int       `json:"round"`
	Session string    `json:"session"`
	Runner  string    `json:"runner,omitempty"`
	Error   string    `json:"error,omitempty"`
	Ended   time.Time `json:"ended,omitzero"`
}

const (
	// workflowDelegateRoundCode marks a launched round's launch record, kept
	// before sdd starts the round's process.
	workflowDelegateRoundCode = "workflow_delegate_round"
	// workflowDelegateRoundProcessCode marks the record of the process a
	// launched round runs in, kept once it runs.
	workflowDelegateRoundProcessCode = "workflow_delegate_round_process"
	// workflowDelegateRoundAgentCode marks the record of the agent CLI a
	// launched round's process started, kept once it runs.
	workflowDelegateRoundAgentCode = "workflow_delegate_round_agent"
)

// delegateRoundStart is a launched round's launch record, with the process
// and the agent CLI recorded for it once they run (zero until then).
type delegateRoundStart struct {
	Round      int           `json:"round"`
	Runner     string        `json:"runner"`
	LaunchedAt time.Time     `json:"launched_at"`
	Timeout    time.Duration `json:"timeout"`
	Log        string        `json:"log"`

	process RoundProcess
	agent   RoundProcess
}

// delegateRoundProcess records a process of a launched round.
type delegateRoundProcess struct {
	Round   int    `json:"round"`
	PID     int    `json:"pid"`
	Started string `json:"started"`
}

// delegateRoundLog is what a child session keeps of its launched rounds.
type delegateRoundLog struct {
	starts   []delegateRoundStart
	outcomes []delegateLaunchRecord
}

func (l delegateRoundLog) start(round int) (delegateRoundStart, bool) {
	if start := l.launched(round); start != nil {
		return *start, true
	}
	return delegateRoundStart{}, false
}

func (l delegateRoundLog) launched(round int) *delegateRoundStart {
	for i := range l.starts {
		if l.starts[i].Round == round {
			return &l.starts[i]
		}
	}
	return nil
}

func (l delegateRoundLog) outcome(round int) (delegateLaunchRecord, bool) {
	for _, outcome := range l.outcomes {
		if outcome.Round == round {
			return outcome, true
		}
	}
	return delegateLaunchRecord{}, false
}

// pending is the earliest launched round without an outcome, nil when none.
func (l delegateRoundLog) pending() *delegateRoundStart {
	for _, start := range l.starts {
		if _, ok := l.outcome(start.Round); !ok {
			return &start
		}
	}
	return nil
}

// next is the number the child's next round takes.
func (l delegateRoundLog) next() int {
	last := 0
	for _, start := range l.starts {
		last = max(last, start.Round)
	}
	for _, outcome := range l.outcomes {
		last = max(last, outcome.Round)
	}
	return last + 1
}

// roundPoll is how often a round's process looks for the record it waits for.
const roundPoll = 250 * time.Millisecond

// workflowCourier carries the engine's deliveries to the other session of a
// fork. A session the current operation already holds receives directly, so
// its in-memory state and stored version stay one.
type workflowCourier struct{ workflow *WorkflowSession }

func (c *workflowCourier) Deliver(d engine.Delivery) (string, error) {
	w := c.workflow
	target := SessionID(d.To.Session)
	if held := w.held[target]; held != nil {
		return held.session.Receive(d)
	}
	for attempt := 0; ; attempt++ {
		linked, refusal, err := w.app.loadLinked(w.ctx, w.identity, target)
		if err != nil || refusal != "" {
			return refusal, err
		}
		refusal, err = linked.session.Receive(d)
		var appErr *ApplicationError
		if err != nil && attempt == 0 && errors.As(err, &appErr) && appErr.Code == ErrorSessionConflict {
			continue
		}
		if err == nil && refusal == "" && linked.session.NonInteractive() {
			w.filled = append(w.filled, d.To)
		}
		return refusal, err
	}
}

// loadLinked replays a session the other side of a fork reaches, without
// stamping an attachment: no client attached to it. An ended session refuses.
func (a *Application) loadLinked(ctx context.Context, identity RequestIdentity, id SessionID) (*WorkflowSession, string, error) {
	_, runtime, stored, err := a.resolveSession(ctx, identity, id)
	if err != nil {
		return nil, "", err
	}
	if stored.Metadata.Ended != nil {
		return nil, fmt.Sprintf("session %s has ended", id), nil
	}
	linked, err := a.replayWorkflow(ctx, identity, runtime, stored)
	return linked, "", err
}

// forkOnAnswer forks the delegate a chooser answer dispatches, in the call
// that answers, so that call's serve carries the dispatch handoff
// (20260829-122157-d-cpt-zpe). A dispatched procedure that is no delivering
// task forks nothing.
func (w *WorkflowSession) forkOnAnswer(ctx context.Context, identity RequestIdentity, parent, procedure string) (*WorkflowDispatch, error) {
	if procedure == "" {
		return nil, nil
	}
	spec, err := w.loadProcedure(procedure)
	if err != nil {
		return nil, err
	}
	if !startsDelegate(spec) {
		return nil, nil
	}
	return w.forkDelegate(ctx, identity, spec, parent)
}

// forkDelegate starts a delivering task in its own child session: the fork is
// recorded on the dispatching instance first, so the child's first delivery
// finds it, then the child starts with the dispatch's seed under the
// dispatching session's branch binding and project target.
func (w *WorkflowSession) forkDelegate(ctx context.Context, identity RequestIdentity, spec *engine.Spec, parent string) (*WorkflowDispatch, error) {
	if spec.Canonical == verifyProcedure {
		// A capture's exchange has one verify delegate: a new exchange's
		// delegate replaces the one an earlier exchange left waiting.
		for _, live := range w.session.LiveForks() {
			if live.Instance != parent || live.Fork.Procedure != spec.Canonical {
				continue
			}
			reason := "a new deep exchange replaced it"
			err := w.session.EndFork(parent, live.Fork.Child, reason)
			if err != nil {
				return nil, err
			}
			err = w.app.endDelegate(ctx, identity, SessionID(live.Fork.Child.Session), reason)
			if err != nil {
				return nil, err
			}
		}
	}
	principal, err := w.app.resolvePrincipal(ctx, identity)
	if err != nil {
		return nil, err
	}
	now := w.app.now().UTC().Round(0)
	created, err := w.app.sessions.Create(ctx, SessionMetadata{
		ID: newWorkflowSessionID(time.Now()), Subject: principal.Subject, Project: w.project,
		Participant: w.session.Participant, Branch: w.branch, UpdatedAt: now,
	})
	if err != nil {
		return nil, err
	}
	childID := created.Metadata.ID
	child, err := w.app.newWorkflow(ctx, identity, w.project, sessionBindingFrom(created), w.branch)
	if err != nil {
		return nil, err
	}
	child.held = map[SessionID]*WorkflowSession{w.ID(): w}
	child.session = child.engine.NewSession(string(childID), w.session.Participant, child.sink,
		engine.WithLineage(engine.Link{Session: string(w.ID()), Instance: parent}), engine.WithCourier(&workflowCourier{workflow: child}))
	if err := child.session.SinkErr(); err != nil {
		return nil, err
	}
	link := engine.Link{Session: string(childID), Instance: child.session.NextInstanceID()}
	seed, err := w.session.Fork(parent, link, spec)
	if err != nil {
		return nil, errors.Join(codedRefusal(err), w.app.endDelegate(ctx, identity, childID, "the fork was refused"))
	}
	child.task = link.Instance
	if project := w.instanceProject(parent); project != w.project {
		child.startProject = project
	}
	if _, err := child.session.Start(spec, seed, ""); err != nil {
		reason := "the delegate failed to start: " + err.Error()
		return nil, errors.Join(err, w.session.EndFork(parent, link, reason), w.app.endDelegate(ctx, identity, childID, reason))
	}
	if err := child.session.SinkErr(); err != nil {
		return nil, err
	}
	dispatch, err := w.dispatchFor(ctx, identity, parent, link, spec.Canonical, true)
	if err != nil {
		// A delegate nobody drives would leave the move waiting on it.
		reason := "the delegate's launch failed: " + err.Error()
		return nil, errors.Join(err, w.session.EndFork(parent, link, reason), w.app.endDelegate(ctx, identity, childID, reason))
	}
	// The delegate runs on the model its dispatch names until it reports one.
	child.setModel(dispatch.Model)
	if err := child.session.SinkErr(); err != nil {
		return nil, err
	}
	return dispatch, nil
}

// dispatchFor composes the dispatch handoff for a child that has a round to
// work, launching the delegate when the composition has a launcher.
func (w *WorkflowSession) dispatchFor(ctx context.Context, identity RequestIdentity, parent string, child engine.Link, procedure string, first bool) (*WorkflowDispatch, error) {
	id := SessionID(child.Session)
	dispatch := &WorkflowDispatch{
		Session: id, Instance: child.Instance, Procedure: procedure,
		Prompt: delegatePrompt(id, child.Instance), ContinuePrompt: delegateContinuePrompt(id, child.Instance), Agent: model.DelegateAgentName,
	}
	runner, err := w.delegateRunner(parent, procedure)
	if err != nil {
		return dispatch, err
	}
	if runner != "" || (w.app.launcher != nil && procedure != verifyProcedure) {
		if w.app.launcher == nil {
			return dispatch, fmt.Errorf("verify.runner %s needs sdd to launch the delegate, and this server launches none: set verify.runner: subagent", runner)
		}
		runtime, err := w.targetRuntime(w.instanceProject(parent), AccessRead)
		if err != nil {
			return dispatch, err
		}
		launched, err := w.app.launchDelegate(ctx, identity, id, child.Instance, procedure, w.instanceProject(parent), runner, runtime.options.Verifier.resolved().RoundTimeout)
		switch {
		case errors.Is(err, ErrDelegateNotLaunched):
		case err != nil:
			return dispatch, err
		default:
			alone := "Delegate works on its own while this move waits: never resume, continue or answer for it. Round runs in the background. Its results arrive in this move, never through a reply. Once a delivery has landed, next with this instance and no report picks it up. If the round fails, next reports the round's failure."
			instructions := "sdd launched the delegate itself: launch nothing. " + alone
			if runner == DelegateLaunchFallback {
				instructions = fmt.Sprintf("Harness has no subagent tool. sdd fell back to the headless %s runner of the agents supported_agents names and launched the delegate itself: launch nothing. %s", launched, alone)
			}
			if first {
				instructions = w.freshVerifierNote(parent, procedure) + instructions
			}
			return &WorkflowDispatch{Procedure: procedure, Runner: launched, Instructions: instructions}, nil
		}
	}
	settings, err := w.subagentSettings(ctx, parent, procedure)
	if err != nil {
		return dispatch, err
	}
	dispatch.Model, dispatch.Effort = settings.Model, settings.Effort
	if first {
		dispatch.Instructions = w.freshVerifierNote(parent, procedure) + "Move dispatched a delegate into its own child session. Launch a fresh-context sub-agent: the " + dispatch.Agent + " subagent sdd init installs for your harness, where it has one. Its prompt is exactly the dispatch prompt. That prompt carries the child handles and nothing else; this dialogue's own session handle never goes into it." + subagentSettingsInstruction(settings) + " Delegate works through the sdd MCP tools only. Its results arrive in this move, never through the sub-agent's reply. Once a delivery has landed, next with this instance and no report picks it up."
		return dispatch, nil
	}
	dispatch.Instructions = "Move sent the delegate its next input. Continue the same delegate through your harness's continue mechanism, with the continue prompt. Harness cannot continue it? Launch a fresh-context " + dispatch.Agent + " sub-agent with the dispatch prompt instead: the child's serve carries the full exchange." + subagentSettingsInstruction(settings) + " This dialogue's own session handle never goes into either prompt. Once the delegate's next delivery has landed, next with this instance and no report picks it up."
	return dispatch, nil
}

// freshVerifierNote is the line a fresh verifier's dispatch opens with when
// it starts after a verdict, saying why the last one takes no revision.
func (w *WorkflowSession) freshVerifierNote(parent, procedure string) string {
	if procedure != verifyProcedure {
		return ""
	}
	inst, ok := w.session.Instance(parent)
	if !ok {
		return ""
	}
	why, _ := workflowStoreString(inst.Store, fieldDeepFresh)
	if why == "" {
		return ""
	}
	return "A fresh verifier reads the draft anew, as " + why + ". "
}

// subagentSettings is the model and effort a harness launches the deep
// verifier's subagent with: the agent block matching the MCP client the
// session last attached through (model.VerifierPolicy.SubagentAgent); empty
// for every other delegate.
func (w *WorkflowSession) subagentSettings(ctx context.Context, parent, procedure string) (VerifierAgent, error) {
	if procedure != verifyProcedure {
		return VerifierAgent{}, nil
	}
	runtime, err := w.targetRuntime(w.instanceProject(parent), AccessRead)
	if err != nil {
		return VerifierAgent{}, err
	}
	stored, err := w.app.sessions.Load(ctx, w.binding.SessionID)
	if err != nil {
		return VerifierAgent{}, err
	}
	client := ""
	if attachment := stored.Metadata.Attachment; attachment != nil {
		client = attachment.ClientName
	}
	return VerifierAgent(runtime.options.Verifier.resolved().SubagentAgent(client)), nil
}

func subagentSettingsInstruction(settings VerifierAgent) string {
	switch {
	case settings.Model != "" && settings.Effort != "":
		return fmt.Sprintf(" Launch it with the dispatch's model %s and effort %s.", settings.Model, settings.Effort)
	case settings.Effort != "":
		return fmt.Sprintf(" Launch it with the dispatch's effort %s.", settings.Effort)
	}
	return ""
}

// delegateRunner is the runner a delegate of procedure takes: for the deep
// verifier, the project's configured headless runner, or the fallback once
// the harness reported no subagent tool (V-D20); empty otherwise.
func (w *WorkflowSession) delegateRunner(parent, procedure string) (string, error) {
	if procedure != verifyProcedure {
		return "", nil
	}
	runtime, err := w.targetRuntime(w.instanceProject(parent), AccessRead)
	if err != nil {
		return "", err
	}
	runner := runtime.options.Verifier.resolved().Runner
	if runner != model.VerifierRunnerSubagent {
		return string(runner), nil
	}
	inst, ok := w.session.Instance(parent)
	if !ok {
		return "", nil
	}
	if fallback, _ := inst.Store.Get(fieldDeepNoSubagent); fallback == true {
		return DelegateLaunchFallback, nil
	}
	return "", nil
}

// filledDispatches composes the handoff for every delegate the current
// operation sent its next input to.
func (w *WorkflowSession) filledDispatches(ctx context.Context, identity RequestIdentity) ([]*WorkflowDispatch, error) {
	filled := w.filled
	w.filled = nil
	var out []*WorkflowDispatch
	for _, child := range filled {
		procedure, parent := "", ""
		for _, live := range w.session.LiveForks() {
			if live.Fork.Child == child {
				procedure, parent = live.Fork.Procedure, live.Instance
			}
		}
		dispatch, err := w.dispatchFor(ctx, identity, parent, child, procedure, false)
		if dispatch != nil {
			out = append(out, dispatch)
		}
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

func delegatePrompt(session SessionID, instance string) string {
	return fmt.Sprintf("sdd delegate. One task, sdd MCP tools only. No sdd CLI, no graph file reads. "+
		"Handles: session %s, instance %s. Only these. Every call names the session; next also the instance. "+
		"First call resume_session: task at its current step, schema to continue, full exchange so far. "+
		"Follow each serve. End your turn only when a serve says it is waiting for the dispatching move. A step with options waits on you. You get continued or relaunched, same handles; next without report picks the input up.", session, instance)
}

func delegateContinuePrompt(session SessionID, instance string) string {
	return fmt.Sprintf("Dispatching move sent next input. Call next with session %s, instance %s, no report. Then continue the task.", session, instance)
}

// launchDelegate hands the delegate's launch to the launcher and, once the
// launcher took it on, starts the round's process and records the round's
// start with the child, which the process waits for; it returns the runner
// that runs.
func (a *Application) launchDelegate(ctx context.Context, identity RequestIdentity, child SessionID, instance, procedure string, project ProjectID, configured string, timeout time.Duration) (string, error) {
	run, err := a.launcher.LaunchDelegate(ctx, DelegateLaunch{Session: child, Instance: instance, Procedure: procedure, Project: project, Runner: configured})
	if err != nil {
		return "", fmt.Errorf("launching the delegate for %s: %w", child, err)
	}
	log, _, err := a.childRounds(ctx, identity, child)
	if err != nil {
		return "", err
	}
	round := LaunchedRound{Session: child, Instance: instance, Procedure: procedure, Project: project, Runner: run.Runner, Number: log.next()}
	// Recorded before the process starts, so a server that dies in between
	// leaves a round the wait step can fail (20261114-160323-d-tac-n0n).
	launched := delegateRoundStart{Round: round.Number, Runner: run.Runner, LaunchedAt: a.now().UTC().Round(0), Timeout: timeout, Log: a.processes.Log(round)}
	recorded, err := a.appendDelegateRecord(ctx, identity, child, workflowDelegateRoundCode, launched, nil)
	if err == nil && !recorded {
		err = fmt.Errorf("the delegate %s ended before its round %d started", child, round.Number)
	}
	if err != nil {
		return "", err
	}
	process, err := a.processes.Start(ctx, round)
	if err != nil {
		return "", fmt.Errorf("starting the process of round %d of the delegate %s: %w", round.Number, child, err)
	}
	recorded, err = a.appendDelegateRecord(ctx, identity, child, workflowDelegateRoundProcessCode, delegateRoundProcess{Round: round.Number, PID: process.PID, Started: process.Started}, roundSettled(round.Number))
	if err == nil && !recorded {
		err = fmt.Errorf("round %d of the delegate %s ended before its process was recorded", round.Number, child)
	}
	if err != nil {
		return "", errors.Join(err, a.processes.Stop(ctx, process, RoundProcess{}))
	}
	return run.Runner, nil
}

// roundSettled holds once round has its outcome.
func roundSettled(round int) func(delegateRoundLog) bool {
	return func(log delegateRoundLog) bool {
		_, ok := log.outcome(round)
		return ok
	}
}

// RunLaunchedRound runs a round sdd launched, in the process RoundProcesses
// started for it, whose PID is pid. It waits for the record of its process and
// for the round before it to record its outcome, whose session it resumes;
// runs the round through the launcher until its deadline; and records the
// outcome with the child: the launcher's session, or the error, which also
// ends the child. A round before it whose process ended without an outcome,
// ran past its timeout or never started fails this one. A child that ended
// meanwhile has no round left and keeps nothing.
func (a *Application) RunLaunchedRound(ctx context.Context, identity RequestIdentity, round LaunchedRound, pid int) error {
	if a.launcher == nil {
		return errors.New("sdd: running a launched round needs the composition's DelegateLauncher")
	}
	start, err := a.awaitRoundStart(ctx, identity, round, pid)
	if err != nil || start == nil {
		return err
	}
	log := slogutils.FromContext(ctx).With("session", round.Session, "round", round.Number, "runner", start.Runner)
	log.Info("running the launched delegate round")
	var previous delegateLaunchRecord
	var roundErr error
	for {
		check, err := a.checkRounds(ctx, identity, round.Session)
		if err != nil || check.ended {
			return err
		}
		if _, done := check.log.outcome(round.Number); done {
			return nil
		}
		if check.pending == nil || check.pending.Round >= round.Number {
			previous, _ = check.log.outcome(round.Number - 1)
			break
		}
		if check.lapse != model.RoundRunning {
			roundErr = errors.New(lapseCause(fmt.Sprintf("round %d", check.pending.Round), check.lapse, *check.pending))
			break
		}
		if err := pause(ctx); err != nil {
			return err
		}
	}
	var end DelegateRoundEnd
	if roundErr == nil {
		end, roundErr, err = a.runRound(ctx, identity, round, *start, previous)
		if err != nil {
			return err
		}
	}
	record := delegateLaunchRecord{Round: round.Number, Session: end.Session, Runner: start.Runner, Ended: a.now().UTC().Round(0)}
	if roundErr != nil {
		record = delegateLaunchRecord{Round: round.Number, Runner: start.Runner, Error: fmt.Sprintf("the delegate's round %d (%s) failed: %v", round.Number, start.Runner, roundErr), Ended: record.Ended}
		log.Warn("the launched delegate round failed", "err", roundErr)
	} else {
		log.Info("the launched delegate round ended", "resume", end.Session)
	}
	recorded, err := a.recordDelegateRound(ctx, identity, round.Session, record)
	if err != nil || !recorded || roundErr == nil {
		return err
	}
	return a.endDelegate(ctx, identity, round.Session, record.Error)
}

// awaitRoundStart waits for the record of the round's process, which names
// this process; nil once the child ended or the round has its outcome first.
func (a *Application) awaitRoundStart(ctx context.Context, identity RequestIdentity, round LaunchedRound, pid int) (*delegateRoundStart, error) {
	limit := a.now().Add(model.RoundOutcomeMargin)
	for {
		log, ended, err := a.childRounds(ctx, identity, round.Session)
		if err != nil || ended || roundSettled(round.Number)(log) {
			return nil, err
		}
		if start, ok := log.start(round.Number); ok && start.process.PID != 0 {
			if start.process.PID != pid {
				return nil, fmt.Errorf("round %d of the delegate %s is recorded with process %d, not with this one (%d)", round.Number, round.Session, start.process.PID, pid)
			}
			return &start, nil
		}
		if a.now().After(limit) {
			return nil, fmt.Errorf("round %d of the delegate %s was not recorded as started within %s", round.Number, round.Session, model.RoundOutcomeMargin)
		}
		if err := pause(ctx); err != nil {
			return nil, err
		}
	}
}

// runRound runs the round through the launcher until its deadline, resuming
// the session the previous round reported. roundErr is why the round failed;
// err is a failure to run it at all.
func (a *Application) runRound(ctx context.Context, identity RequestIdentity, round LaunchedRound, start delegateRoundStart, previous delegateLaunchRecord) (end DelegateRoundEnd, roundErr, err error) {
	run, launchErr := a.launcher.LaunchDelegate(ctx, DelegateLaunch{Session: round.Session, Instance: round.Instance, Procedure: round.Procedure, Project: round.Project, Runner: start.Runner})
	if launchErr != nil {
		return DelegateRoundEnd{}, launchErr, nil
	}
	_, _, stored, err := a.resolveSession(ctx, identity, round.Session)
	if err != nil {
		return DelegateRoundEnd{}, nil, err
	}
	delivered, err := delegateDeliveries(stored.Events)
	if err != nil {
		return DelegateRoundEnd{}, nil, err
	}
	next := DelegateRound{Number: round.Number, Prompt: delegatePrompt(round.Session, round.Instance)}
	next.AgentStarted = func(ctx context.Context, agent RoundProcess) error {
		recorded, err := a.appendDelegateRecord(ctx, identity, round.Session, workflowDelegateRoundAgentCode, delegateRoundProcess{Round: round.Number, PID: agent.PID, Started: agent.Started}, roundSettled(round.Number))
		if err == nil && !recorded {
			err = fmt.Errorf("round %d of the delegate %s ended before its agent CLI was recorded", round.Number, round.Session)
		}
		return err
	}
	if previous.Session != "" {
		next.Resume = previous.Session
		next.Prompt = delegateContinuePrompt(round.Session, round.Instance)
	}
	deadline := model.RoundDeadline(start.LaunchedAt, previous.Ended, start.Timeout)
	end, roundErr = roundWithin(ctx, run, next, deadline.Sub(a.now()), start.Timeout)
	if roundErr != nil {
		return end, roundErr, nil
	}
	idle, err := a.idleRound(ctx, identity, round.Session, delivered, end.Message)
	if err != nil || idle == "" {
		return end, nil, err
	}
	return end, errors.New(idle), nil
}

// lapseCause names why the launched round that recorded no outcome failed,
// and its process's log; subject names the round.
func lapseCause(subject string, lapse model.RoundLapse, round delegateRoundStart) string {
	var cause string
	switch lapse {
	case model.RoundPastTimeout:
		cause = fmt.Sprintf("%s ran past verify.round_timeout (%s) without recording an outcome", subject, round.Timeout)
	case model.RoundNeverStarted:
		cause = fmt.Sprintf("%s's process never started: it was not recorded as running within %s of the launch", subject, model.RoundOutcomeMargin)
	default:
		cause = subject + "'s process ended without recording an outcome"
	}
	return fmt.Sprintf("%s (its log: %s)", cause, round.Log)
}

func pause(ctx context.Context) error {
	timer := time.NewTimer(roundPoll)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// roundCheck is a child's earliest launched round without an outcome, and
// whether it lapsed.
type roundCheck struct {
	ended   bool
	log     delegateRoundLog
	pending *delegateRoundStart
	lapse   model.RoundLapse
}

// checkRounds decides the child's earliest round without an outcome by its
// process and its deadline.
func (a *Application) checkRounds(ctx context.Context, identity RequestIdentity, child SessionID) (roundCheck, error) {
	log, ended, err := a.childRounds(ctx, identity, child)
	if err != nil || ended {
		return roundCheck{ended: ended, log: log}, err
	}
	pending := log.pending()
	if pending == nil {
		return roundCheck{log: log}, nil
	}
	started := pending.process.PID != 0
	alive := false
	if started {
		if a.processes == nil {
			return roundCheck{}, fmt.Errorf("round %d of the delegate %s runs in a process this composition cannot check: it has no RoundProcesses", pending.Round, child)
		}
		alive, err = a.processes.Alive(ctx, pending.process)
		if err != nil {
			return roundCheck{}, fmt.Errorf("checking the process of round %d of the delegate %s: %w", pending.Round, child, err)
		}
	}
	// Read again: a process that recorded its outcome and ended in between
	// did not lapse, nor did one recorded as running in between.
	log, ended, err = a.childRounds(ctx, identity, child)
	if err != nil || ended {
		return roundCheck{ended: ended, log: log}, err
	}
	if again := log.pending(); again == nil || again.Round != pending.Round || again.process != pending.process {
		return roundCheck{log: log, pending: again}, nil
	}
	previous, _ := log.outcome(pending.Round - 1)
	round := model.PendingRound{Launched: pending.LaunchedAt, Deadline: model.RoundDeadline(pending.LaunchedAt, previous.Ended, pending.Timeout), Started: started, Alive: alive}
	return roundCheck{log: log, pending: pending, lapse: model.LapsedRound(round, a.now())}, nil
}

// failLapsedRound fails the child's round that lapsed without an outcome as
// its process would have: the error recorded and the child ended. A round
// whose process was recorded is stopped first: one past its timeout with its
// process group, one whose process is gone with the agent CLI it left
// running.
func (a *Application) failLapsedRound(ctx context.Context, identity RequestIdentity, child SessionID) error {
	check, err := a.checkRounds(ctx, identity, child)
	if err != nil || check.ended || check.pending == nil || check.lapse == model.RoundRunning {
		return err
	}
	pending := check.pending
	if check.lapse == model.RoundPastTimeout || check.lapse == model.RoundProcessGone {
		if err := a.processes.Stop(ctx, pending.process, pending.agent); err != nil {
			return fmt.Errorf("stopping the process of round %d of the delegate %s: %w", pending.Round, child, err)
		}
	}
	record := delegateLaunchRecord{Round: pending.Round, Runner: pending.Runner, Ended: a.now().UTC().Round(0),
		Error: fmt.Sprintf("the delegate's round %d (%s) failed: %s", pending.Round, pending.Runner, lapseCause("the round", check.lapse, *pending))}
	recorded, err := a.recordDelegateRound(ctx, identity, child, record)
	if err != nil || !recorded {
		return err
	}
	return a.endDelegate(ctx, identity, child, record.Error)
}

// stopRounds stops the process of every round of the ended child events
// belong to that recorded no outcome. A round whose process was never
// recorded has none to stop: a process that runs unrecorded finds the child
// ended and runs nothing.
func (a *Application) stopRounds(ctx context.Context, child SessionID, events []StoredEvent) error {
	log, err := delegateRoundsOf(events)
	if err != nil {
		return err
	}
	var failures []error
	for _, start := range log.starts {
		if _, ok := log.outcome(start.Round); ok || start.process.PID == 0 {
			continue
		}
		if a.processes == nil {
			return fmt.Errorf("round %d of the delegate %s runs in a process this composition cannot stop: it has no RoundProcesses", start.Round, child)
		}
		if err := a.processes.Stop(ctx, start.process, start.agent); err != nil {
			failures = append(failures, fmt.Errorf("stopping the process of round %d of the delegate %s: %w", start.Round, child, err))
		}
	}
	return errors.Join(failures...)
}

// childRounds reads what the child keeps of its launched rounds, and whether
// it ended.
func (a *Application) childRounds(ctx context.Context, identity RequestIdentity, child SessionID) (delegateRoundLog, bool, error) {
	_, _, stored, err := a.resolveSession(ctx, identity, child)
	if err != nil {
		return delegateRoundLog{}, false, err
	}
	log, err := delegateRoundsOf(stored.Events)
	return log, stored.Metadata.Ended != nil, err
}

// roundWithin runs one round, stopped once it runs for longer than remaining,
// what its timeout leaves it: its context ends, which stops a headless
// runner's process, and the round fails without waiting on a runner that
// keeps running.
func roundWithin(ctx context.Context, run DelegateRun, round DelegateRound, remaining, timeout time.Duration) (DelegateRoundEnd, error) {
	ctx, cancel := context.WithTimeout(ctx, remaining)
	defer cancel()
	type ended struct {
		end DelegateRoundEnd
		err error
	}
	done := make(chan ended, 1)
	go func() {
		end, err := run.Round(ctx, round)
		done <- ended{end, err}
	}()
	select {
	case e := <-done:
		if ctx.Err() == nil {
			return e.end, e.err
		}
	case <-ctx.Done():
	}
	return DelegateRoundEnd{}, fmt.Errorf("it ran past verify.round_timeout (%s) and was stopped", timeout)
}

// idleRound names why a round that ended cleanly failed: its child, which had
// made delivered deliveries before it, delivered nothing more and still runs.
// It is empty once the child delivered or concluded.
func (a *Application) idleRound(ctx context.Context, identity RequestIdentity, child SessionID, delivered int, message string) (string, error) {
	_, _, stored, err := a.resolveSession(ctx, identity, child)
	if err != nil {
		return "", err
	}
	if stored.Metadata.Ended != nil {
		return "", nil
	}
	now, err := delegateDeliveries(stored.Events)
	if err != nil || now > delivered {
		return "", err
	}
	said := "it left no last message"
	if message = strings.TrimSpace(message); message != "" {
		said = "its last message: " + message
	}
	return "it ended without the delegate delivering or concluding; " + said, nil
}

// delegateDeliveries counts the deliveries a child session made.
func delegateDeliveries(events []StoredEvent) (int, error) {
	count := 0
	for _, stored := range events {
		if stored.Code != WorkflowEventCode {
			continue
		}
		var event engine.Event
		if err := json.Unmarshal(stored.Payload, &event); err != nil {
			return 0, fmt.Errorf("decoding a delegate session event: %w", err)
		}
		if event.Event == engine.EventDelivered {
			count++
		}
	}
	return count, nil
}

// recordDelegateRound appends a round's outcome to the child session,
// reporting whether it did: a child that ended, or a round that has its
// outcome already, keeps no other.
func (a *Application) recordDelegateRound(ctx context.Context, identity RequestIdentity, child SessionID, record delegateLaunchRecord) (bool, error) {
	return a.appendDelegateRecord(ctx, identity, child, workflowDelegateLaunchCode, record, roundSettled(record.Round))
}

// appendDelegateRecord appends record under code to the child session,
// unless the child ended or kept holds for what it keeps already; it reports
// whether it appended.
func (a *Application) appendDelegateRecord(ctx context.Context, identity RequestIdentity, child SessionID, code string, record any, kept func(delegateRoundLog) bool) (bool, error) {
	payload, err := json.Marshal(record)
	if err != nil {
		return false, err
	}
	// The delegate's agent and the round's process write the child too.
	const attempts = 3
	for attempt := 1; ; attempt++ {
		_, _, stored, err := a.resolveSession(ctx, identity, child)
		if err != nil {
			return false, err
		}
		if stored.Metadata.Ended != nil {
			return false, nil
		}
		if kept != nil {
			log, err := delegateRoundsOf(stored.Events)
			if err != nil || kept(log) {
				return false, err
			}
		}
		metadata := stored.Metadata
		metadata.UpdatedAt = a.now().UTC().Round(0)
		_, err = a.sessions.Append(ctx, child, stored.Version, SessionAppend{Metadata: &metadata, Events: []StoredEvent{{CodecVersion: SessionCodecVersion, Code: code, Payload: payload}}})
		if attempt < attempts && isSessionConflict(err) {
			continue
		}
		return err == nil, err
	}
}

// failedRounds ends the fork of a delegate the instance dispatched whose
// launched round failed, so the move can report the failure and dispatch
// afresh. A round without an outcome whose process is gone, or that ran past
// its timeout, fails here. The call fails with the round's error, unless the
// move waits at a gate whose delegateEnded route takes the failure on.
func (w *WorkflowSession) failedRounds(ctx context.Context, identity RequestIdentity, instance string) error {
	routed := false
	if inst, ok := w.session.Instance(instance); ok {
		step := inst.Spec.StepByID[inst.Step]
		routed = step != nil && len(step.Await) > 0 && step.DelegateEnded != ""
	}
	var failures []error
	for _, live := range w.session.LiveForks() {
		if live.Instance != instance {
			continue
		}
		child := SessionID(live.Fork.Child.Session)
		if err := w.app.failLapsedRound(ctx, identity, child); err != nil {
			return err
		}
		_, _, stored, err := w.app.resolveSession(ctx, identity, child)
		if err != nil {
			return err
		}
		records, err := delegateLaunches(stored.Events)
		if err != nil {
			return err
		}
		n := len(records)
		if n == 0 || records[n-1].Error == "" {
			continue
		}
		if err := w.session.EndFork(instance, live.Fork.Child, records[n-1].Error); err != nil {
			return err
		}
		if !routed {
			failures = append(failures, errors.New(records[n-1].Error))
		}
	}
	return errors.Join(failures...)
}

func delegateLaunches(events []StoredEvent) ([]delegateLaunchRecord, error) {
	log, err := delegateRoundsOf(events)
	return log.outcomes, err
}

func delegateRoundsOf(events []StoredEvent) (delegateRoundLog, error) {
	var log delegateRoundLog
	for _, stored := range events {
		switch stored.Code {
		case workflowDelegateLaunchCode, workflowDelegateRoundCode, workflowDelegateRoundProcessCode, workflowDelegateRoundAgentCode:
		default:
			continue
		}
		if !SupportedSessionCodecVersion(stored.CodecVersion) {
			return delegateRoundLog{}, &ApplicationError{Code: ErrorMigrationRequired, Message: "unsupported delegate launch event codec", Version: stored.CodecVersion}
		}
		switch stored.Code {
		case workflowDelegateRoundCode:
			var start delegateRoundStart
			if err := json.Unmarshal(stored.Payload, &start); err != nil {
				return delegateRoundLog{}, fmt.Errorf("decoding delegate round start event: %w", err)
			}
			log.starts = append(log.starts, start)
		case workflowDelegateRoundProcessCode, workflowDelegateRoundAgentCode:
			var process delegateRoundProcess
			if err := json.Unmarshal(stored.Payload, &process); err != nil {
				return delegateRoundLog{}, fmt.Errorf("decoding delegate round process event: %w", err)
			}
			start := log.launched(process.Round)
			if start == nil {
				return delegateRoundLog{}, fmt.Errorf("a process of round %d is recorded before the round's launch", process.Round)
			}
			recorded := RoundProcess{PID: process.PID, Started: process.Started}
			if stored.Code == workflowDelegateRoundAgentCode {
				start.agent = recorded
			} else {
				start.process = recorded
			}
		default:
			var record delegateLaunchRecord
			if err := json.Unmarshal(stored.Payload, &record); err != nil {
				return delegateRoundLog{}, fmt.Errorf("decoding delegate launch event: %w", err)
			}
			log.outcomes = append(log.outcomes, record)
		}
	}
	return log, nil
}

// DelegateLaunches lists the launcher sessions kept with a delegate child
// session, one per launched round in order.
func (a *Application) DelegateLaunches(ctx context.Context, identity RequestIdentity, child SessionID) ([]string, error) {
	_, _, stored, err := a.resolveSession(ctx, identity, child)
	if err != nil {
		return nil, err
	}
	records, err := delegateLaunches(stored.Events)
	if err != nil {
		return nil, err
	}
	sessions := make([]string, 0, len(records))
	for _, record := range records {
		sessions = append(sessions, record.Session)
	}
	return sessions, nil
}

// concludeEndedForks ends every live delegate whose dispatching move is no
// longer running, or every live delegate once the dispatching dialogue ended:
// a delegate lives no longer than the move it works for.
func (w *WorkflowSession) concludeEndedForks(ctx context.Context, identity RequestIdentity) error {
	finished := w.Finished()
	for _, live := range w.session.LiveForks() {
		inst, ok := w.session.Instance(live.Instance)
		if !ok || (inst.Status == engine.StatusRunning && !finished) {
			continue
		}
		reason := fmt.Sprintf("the dispatching move %s ended (%s)", inst.Spec.Canonical, inst.Outcome)
		if inst.Status == engine.StatusRunning {
			reason = "the dispatching session ended"
		}
		if err := w.app.endDelegate(ctx, identity, SessionID(live.Fork.Child.Session), reason); err != nil {
			return err
		}
		if !finished {
			if err := w.session.EndFork(live.Instance, live.Fork.Child, reason); err != nil {
				return err
			}
		}
	}
	return nil
}

// endDelegate tears a delegate child session down; one already ended is left
// as it is.
func (a *Application) endDelegate(ctx context.Context, identity RequestIdentity, child SessionID, reason string) error {
	for attempt := 0; ; attempt++ {
		_, err := a.abandonSession(ctx, identity, WorkflowResumeRequest{SessionID: child}, reason, false)
		var appErr *ApplicationError
		if errors.As(err, &appErr) && appErr.Code == ErrorSessionEnded {
			return nil
		}
		// The delegate may write its session meanwhile, as a background
		// round's agent does.
		if attempt == 0 && errors.As(err, &appErr) && appErr.Code == ErrorSessionConflict {
			continue
		}
		return err
	}
}

// releaseFork tells the dispatching session that a delegate torn down by
// handle stopped, so its fork no longer reads live there.
func (a *Application) releaseFork(ctx context.Context, identity RequestIdentity, lineage engine.Link, child engine.Link, reason string) error {
	parent, refusal, err := a.loadLinked(ctx, identity, SessionID(lineage.Session))
	if err != nil || refusal != "" {
		return err
	}
	if _, ok := parent.session.Instance(lineage.Instance); !ok {
		return nil
	}
	return parent.session.EndFork(lineage.Instance, child, reason)
}

// delegateEndNote is what a delegate is told once its task has ended.
func delegateEndNote(serve *engine.Serve) string {
	if serve.Status == engine.StatusCompleted {
		return "Final result delivered to the dispatching move. Delegate session ended. Stop here."
	}
	return "Delegate session concluded; its work is over. Stop here."
}

// startsDelegate reports whether starting spec forks a delegate child session.
func startsDelegate(spec *engine.Spec) bool {
	return spec.Class == model.ProcedureClassTask && spec.Delegates()
}
