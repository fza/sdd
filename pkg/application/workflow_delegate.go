package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
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
// harness, and sdd runs the round it returns in the background, so the
// dispatching call never waits for a round: that move's next call picks the
// delegate's deliveries up, and fails with the round's error when the round
// failed.
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
	// Launch is the launch the project configures for this delegate: claude,
	// codex or custom_driver, or DelegateLaunchFallback where the harness
	// reported no subagent tool; empty leaves the launch to the composition.
	Launch string
}

// DelegateRun is a launch a launcher took on: the launch that runs, and Round,
// which runs one round to its end, bounded by the launcher, and reports the
// launcher's own session for the next round to resume. sdd runs one round of
// a child at a time.
type DelegateRun struct {
	Launch string
	Round  func(ctx context.Context, round DelegateRound) (string, error)
}

// DelegateRound is one round's input: its number from 1, its prompt, and the
// session the launcher reported for the previous round, empty on the first.
type DelegateRound struct {
	Number int
	Prompt string
	Resume string
}

// DelegateLaunchFallback asks the launcher for the first headless launch of
// the agents the project's supported_agents name whose CLI is found
// (20261109-004729-d-cpt-xs5, V-D20).
const DelegateLaunchFallback = "fallback"

// ErrDelegateNotLaunched is what a launcher returns for a delegate it leaves
// to the harness: the dispatch then serves the harness's launch instructions.
var ErrDelegateNotLaunched = errors.New("the delegate is left to the harness to launch")

// WorkflowDispatch is the dispatch handoff served to the dispatching move: the
// child handles, the prompts a harness launches the delegate with, and the
// instructions for this round (20260829-122157-d-cpt-zpe).
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
	// verify.subagent; empty leaves the harness's own.
	Model  string
	Effort string
	// Launch is the launch sdd runs the delegate's round with, when it launched
	// the delegate itself.
	Launch string
}

// delegateLaunchRecord is one launched round's outcome, kept with the child
// session: the launcher's session to resume, or the error the round failed
// with.
type delegateLaunchRecord struct {
	Round   int    `json:"round"`
	Session string `json:"session"`
	Launch  string `json:"launch,omitempty"`
	Error   string `json:"error,omitempty"`
}

// delegateRounds orders the background rounds of each child: a round starts
// once the child's previous round ended, so it resumes that round's session.
type delegateRounds struct {
	mu   sync.Mutex
	last map[SessionID]chan struct{}
}

// follow registers done as the child's latest round and returns the round it
// waits for, nil when none.
func (r *delegateRounds) follow(child SessionID, done chan struct{}) chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.last == nil {
		r.last = map[SessionID]chan struct{}{}
	}
	previous := r.last[child]
	r.last[child] = done
	return previous
}

// settle forgets the child's rounds once done is still its latest.
func (r *delegateRounds) settle(child SessionID, done chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.last[child] == done {
		delete(r.last, child)
	}
}

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
	launch, err := w.delegateLaunch(parent, procedure)
	if err != nil {
		return dispatch, err
	}
	if launch != "" || (w.app.launcher != nil && procedure != verifyProcedure) {
		if w.app.launcher == nil {
			return dispatch, fmt.Errorf("verify.launch %s needs sdd to launch the delegate, and this server launches none: set verify.launch: subagent", launch)
		}
		launched, err := w.app.launchDelegate(ctx, identity, id, child.Instance, procedure, w.instanceProject(parent), launch)
		switch {
		case errors.Is(err, ErrDelegateNotLaunched):
		case err != nil:
			return dispatch, err
		default:
			dispatch.Launch = launched
			arrives := "The round runs in the background. Its results arrive in this move, never through a reply: next with this instance and no report picks a delivery up once it has landed, and fails with the round's error should the round fail."
			dispatch.Instructions = "sdd launched the delegate itself, so launch nothing. " + arrives
			if launch == DelegateLaunchFallback {
				dispatch.Instructions = fmt.Sprintf("This harness has no subagent tool, so sdd fell back to the headless %s launch of the agents supported_agents names and launched the delegate itself: launch nothing. %s", launched, arrives)
			}
			return dispatch, nil
		}
	}
	settings, err := w.subagentSettings(parent, procedure)
	if err != nil {
		return dispatch, err
	}
	dispatch.Model, dispatch.Effort = settings.Model, settings.Effort
	if first {
		dispatch.Instructions = "This move dispatched a delegate into its own child session. Launch a fresh-context sub-agent, the " + dispatch.Agent + " subagent sdd init installs for your harness where it has one, whose prompt is exactly the dispatch prompt: the prompt carries the child handles and nothing else, and this dialogue's own session handle never goes into it." + subagentSettingsInstruction(settings) + " The delegate works through the sdd MCP tools only. Its results arrive in this move, never through the sub-agent's reply: next with this instance and no report picks a delivery up once it has landed."
		return dispatch, nil
	}
	dispatch.Instructions = "This move sent the delegate its next input. Continue the same delegate through your harness's continue mechanism with the continue prompt; where the harness cannot continue it, launch a fresh-context " + dispatch.Agent + " sub-agent with the dispatch prompt instead, since the child's serve carries the full exchange." + subagentSettingsInstruction(settings) + " This dialogue's own session handle never goes into either prompt. Next with this instance and no report picks the delegate's next delivery up once it has landed."
	return dispatch, nil
}

// subagentSettings is the model and effort a harness launches the deep
// verifier's subagent with, from verify.subagent; empty for every other
// delegate.
func (w *WorkflowSession) subagentSettings(parent, procedure string) (VerifierAgent, error) {
	if procedure != verifyProcedure {
		return VerifierAgent{}, nil
	}
	runtime, err := w.targetRuntime(w.instanceProject(parent), AccessRead)
	if err != nil {
		return VerifierAgent{}, err
	}
	return runtime.options.Verifier.Subagent, nil
}

func subagentSettingsInstruction(settings VerifierAgent) string {
	switch {
	case settings.Model != "" && settings.Effort != "":
		return fmt.Sprintf(" Launch it with model %s and effort %s, the dispatch's model and effort.", settings.Model, settings.Effort)
	case settings.Model != "":
		return fmt.Sprintf(" Launch it with model %s, the dispatch's model.", settings.Model)
	case settings.Effort != "":
		return fmt.Sprintf(" Launch it with effort %s, the dispatch's effort.", settings.Effort)
	}
	return ""
}

// delegateLaunch is the launch a delegate of procedure takes: for the deep
// verifier, the project's configured headless launch, or the fallback once
// the harness reported no subagent tool (V-D20); empty otherwise.
func (w *WorkflowSession) delegateLaunch(parent, procedure string) (string, error) {
	if procedure != verifyProcedure {
		return "", nil
	}
	runtime, err := w.targetRuntime(w.instanceProject(parent), AccessRead)
	if err != nil {
		return "", err
	}
	launch := runtime.options.Verifier.resolved().Launch
	if launch != model.VerifierLaunchSubagent {
		return string(launch), nil
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
	return fmt.Sprintf("You are a delegate working one sdd task through the sdd MCP tools only: no sdd CLI, no reading graph files. "+
		"Your child session handle is %s and your instance is %s; they are the only handles you hold. "+
		"First call resume_session with session %s: it serves the task at its current step with the schema to continue it and the full exchange so far. "+
		"Follow what each serve says. When a step waits for the dispatching move, end your turn; once it has sent, you are continued or relaunched with these same handles, "+
		"and next with session %s, instance %s and no report picks its input up.", session, instance, session, session, instance)
}

func delegateContinuePrompt(session SessionID, instance string) string {
	return fmt.Sprintf("The dispatching move has sent its next input. Call next with session %s, instance %s and no report to pick it up, then continue the task.", session, instance)
}

// launchDelegate hands the delegate's launch to the launcher and, once the
// launcher took it on, runs its round in the background, outliving the call
// that launched it; it returns the launch that runs.
func (a *Application) launchDelegate(ctx context.Context, identity RequestIdentity, child SessionID, instance, procedure string, project ProjectID, configured string) (string, error) {
	run, err := a.launcher.LaunchDelegate(ctx, DelegateLaunch{Session: child, Instance: instance, Procedure: procedure, Project: project, Launch: configured})
	if err != nil {
		return "", fmt.Errorf("launching the delegate for %s: %w", child, err)
	}
	done := make(chan struct{})
	previous := a.rounds.follow(child, done)
	background := context.WithoutCancel(ctx)
	go func() {
		defer close(done)
		defer a.rounds.settle(child, done)
		if previous != nil {
			<-previous
		}
		err := a.runDelegateRound(background, identity, child, instance, run)
		if err != nil {
			slogutils.FromContext(background).Error("recording a delegate round", "session", child, "error", err)
		}
	}()
	return run.Launch, nil
}

// runDelegateRound runs one round and keeps its outcome with the child: the
// launcher's session, so the next round resumes it, or the error, which the
// dispatching move's next call fails with. A failed round ends the child. A
// child that ended during the round keeps nothing: it has no round left.
func (a *Application) runDelegateRound(ctx context.Context, identity RequestIdentity, child SessionID, instance string, run DelegateRun) error {
	_, _, stored, err := a.resolveSession(ctx, identity, child)
	if err != nil {
		return err
	}
	records, err := delegateLaunches(stored.Events)
	if err != nil {
		return err
	}
	round := DelegateRound{Number: len(records) + 1, Prompt: delegatePrompt(child, instance)}
	if n := len(records); n > 0 && records[n-1].Session != "" {
		round.Resume = records[n-1].Session
		round.Prompt = delegateContinuePrompt(child, instance)
	}
	session, roundErr := run.Round(ctx, round)
	record := delegateLaunchRecord{Round: round.Number, Session: session, Launch: run.Launch}
	if roundErr != nil {
		record = delegateLaunchRecord{Round: round.Number, Launch: run.Launch, Error: fmt.Sprintf("the delegate's round %d (%s) failed: %v", round.Number, run.Launch, roundErr)}
	}
	ended, err := a.recordDelegateRound(ctx, identity, child, record)
	if err != nil || ended || roundErr == nil {
		return err
	}
	return a.endDelegate(ctx, identity, child, record.Error)
}

// recordDelegateRound appends a round's outcome to the child session,
// reporting whether the child had ended already.
func (a *Application) recordDelegateRound(ctx context.Context, identity RequestIdentity, child SessionID, record delegateLaunchRecord) (bool, error) {
	payload, err := json.Marshal(record)
	if err != nil {
		return false, err
	}
	for attempt := 0; ; attempt++ {
		_, _, stored, err := a.resolveSession(ctx, identity, child)
		if err != nil {
			return false, err
		}
		if stored.Metadata.Ended != nil {
			return true, nil
		}
		metadata := stored.Metadata
		metadata.UpdatedAt = a.now().UTC().Round(0)
		_, err = a.sessions.Append(ctx, child, stored.Version, SessionAppend{Metadata: &metadata, Events: []StoredEvent{{CodecVersion: SessionCodecVersion, Code: workflowDelegateLaunchCode, Payload: payload}}})
		var appErr *ApplicationError
		if err != nil && attempt == 0 && errors.As(err, &appErr) && appErr.Code == ErrorSessionConflict {
			continue
		}
		return false, err
	}
}

// failedRounds fails the call when a background round of a delegate the
// instance dispatched failed: the fork ends, so the move can report the
// failure and dispatch afresh.
func (w *WorkflowSession) failedRounds(ctx context.Context, identity RequestIdentity, instance string) error {
	var failures []error
	for _, live := range w.session.LiveForks() {
		if live.Instance != instance {
			continue
		}
		_, _, stored, err := w.app.resolveSession(ctx, identity, SessionID(live.Fork.Child.Session))
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
		failures = append(failures, errors.New(records[n-1].Error))
	}
	return errors.Join(failures...)
}

func delegateLaunches(events []StoredEvent) ([]delegateLaunchRecord, error) {
	var records []delegateLaunchRecord
	for _, stored := range events {
		if stored.Code != workflowDelegateLaunchCode {
			continue
		}
		if !SupportedSessionCodecVersion(stored.CodecVersion) {
			return nil, &ApplicationError{Code: ErrorMigrationRequired, Message: "unsupported delegate launch event codec", Version: stored.CodecVersion}
		}
		var record delegateLaunchRecord
		if err := json.Unmarshal(stored.Payload, &record); err != nil {
			return nil, fmt.Errorf("decoding delegate launch event: %w", err)
		}
		records = append(records, record)
	}
	return records, nil
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
		return "Your final result is delivered to the dispatching move and this delegate session has ended. Stop here."
	}
	return "This delegate session has concluded and its work is over. Stop here."
}

// startsDelegate reports whether starting spec forks a delegate child session.
func startsDelegate(spec *engine.Spec) bool {
	return spec.Class == model.ProcedureClassTask && spec.Delegates()
}
