package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"time"

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
// next input. A launcher that waits for the round to end leaves the caller's
// view of the dispatching session behind the deliveries the delegate made;
// that session's next call reads them.
type DelegateLauncher interface {
	LaunchDelegate(ctx context.Context, launch DelegateLaunch) (DelegateLaunched, error)
}

// DelegateLaunch is one round's launch: the child handles, the prompt, and
// the launcher's own session from the previous round. It carries nothing of
// the dispatching session.
type DelegateLaunch struct {
	Session   SessionID
	Instance  string
	Procedure string
	// Round counts this child's launches, from 1.
	Round  int
	Prompt string
	// Resume is the session the launcher reported for the previous round;
	// empty on the first.
	Resume string
}

// DelegateLaunched is what a launch reports back: the launcher's own session
// identity, kept with the child session to resume it next round.
type DelegateLaunched struct {
	Session string
}

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
	// Launched is the launcher's session when sdd launched the delegate itself.
	Launched string
}

type delegateLaunchRecord struct {
	Round   int    `json:"round"`
	Session string `json:"session"`
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

// forkDelegate starts a delivering task in its own child session: the fork is
// recorded on the dispatching instance first, so the child's first delivery
// finds it, then the child starts with the dispatch's seed under the
// dispatching session's branch binding and project target.
func (w *WorkflowSession) forkDelegate(ctx context.Context, identity RequestIdentity, spec *engine.Spec, request WorkflowStartRequest, parent string) (*WorkflowServe, error) {
	if request.Project != "" && request.Project != w.instanceProject(parent) {
		return nil, fmt.Errorf("a delegate works in the project of the move that dispatches it; %s targets %s", parent, w.instanceProject(parent))
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
	params := maps.Clone(seed)
	if params == nil {
		params = map[string]any{}
	}
	maps.Copy(params, request.Params)
	child.task = link.Instance
	if project := w.instanceProject(parent); project != w.project {
		child.startProject = project
	}
	if _, err := child.session.Start(spec, params, ""); err != nil {
		reason := "the delegate failed to start: " + err.Error()
		return nil, errors.Join(err, w.session.EndFork(parent, link, reason), w.app.endDelegate(ctx, identity, childID, reason))
	}
	if err := child.session.SinkErr(); err != nil {
		return nil, err
	}
	serve, err := w.session.Serve(parent)
	if err != nil {
		return nil, err
	}
	result, err := w.publicServe(serve)
	if err != nil {
		return nil, err
	}
	dispatch, err := w.dispatchFor(ctx, identity, link, spec.Canonical, true)
	result.Dispatches = append(result.Dispatches, dispatch)
	return result, err
}

// dispatchFor composes the dispatch handoff for a child that has a round to
// work, launching the delegate when the composition has a launcher.
func (w *WorkflowSession) dispatchFor(ctx context.Context, identity RequestIdentity, child engine.Link, procedure string, first bool) (*WorkflowDispatch, error) {
	id := SessionID(child.Session)
	dispatch := &WorkflowDispatch{
		Session: id, Instance: child.Instance, Procedure: procedure,
		Prompt: delegatePrompt(id, child.Instance), ContinuePrompt: delegateContinuePrompt(id, child.Instance),
	}
	switch {
	case w.app.launcher != nil:
		launched, err := w.app.launchDelegate(ctx, identity, id, child.Instance, procedure)
		if err != nil {
			return dispatch, err
		}
		dispatch.Launched = launched
		dispatch.Instructions = "sdd launched the delegate itself, so launch nothing. Its results arrive in this move, never through a reply: next with this instance and no report picks a delivery up once it has landed."
	case first:
		dispatch.Instructions = "This move dispatched a delegate into its own child session. Launch a fresh-context sub-agent whose prompt is exactly the dispatch prompt: it carries the child handles and nothing else, and this dialogue's own session handle never goes into it. The delegate works through the sdd MCP tools only. Its results arrive in this move, never through the sub-agent's reply: next with this instance and no report picks a delivery up once it has landed."
	default:
		dispatch.Instructions = "This move sent the delegate its next input. Continue the same delegate through your harness's continue mechanism with the continue prompt; where the harness cannot continue it, launch a fresh-context sub-agent with the dispatch prompt instead, since the child's serve carries the full exchange. This dialogue's own session handle never goes into either prompt. Next with this instance and no report picks the delegate's next delivery up once it has landed."
	}
	return dispatch, nil
}

// filledDispatches composes the handoff for every delegate the current
// operation sent its next input to.
func (w *WorkflowSession) filledDispatches(ctx context.Context, identity RequestIdentity) ([]*WorkflowDispatch, error) {
	filled := w.filled
	w.filled = nil
	var out []*WorkflowDispatch
	for _, child := range filled {
		procedure := ""
		for _, live := range w.session.LiveForks() {
			if live.Fork.Child == child {
				procedure = live.Fork.Procedure
			}
		}
		dispatch, err := w.dispatchFor(ctx, identity, child, procedure, false)
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
		"Follow each serve. Step waits on the dispatching move: end turn. You get continued or relaunched, same handles; next without report picks the input up.", session, instance)
}

func delegateContinuePrompt(session SessionID, instance string) string {
	return fmt.Sprintf("Dispatching move sent next input. Call next with session %s, instance %s, no report. Then continue the task.", session, instance)
}

// launchDelegate runs one round's launch and keeps the launcher's session with
// the child, so the next round resumes it. A child that ended during the
// launch keeps nothing: it has no round left to resume.
func (a *Application) launchDelegate(ctx context.Context, identity RequestIdentity, child SessionID, instance, procedure string) (string, error) {
	_, _, stored, err := a.resolveSession(ctx, identity, child)
	if err != nil {
		return "", err
	}
	records, err := delegateLaunches(stored.Events)
	if err != nil {
		return "", err
	}
	launch := DelegateLaunch{Session: child, Instance: instance, Procedure: procedure, Round: len(records) + 1, Prompt: delegatePrompt(child, instance)}
	if n := len(records); n > 0 && records[n-1].Session != "" {
		launch.Resume = records[n-1].Session
		launch.Prompt = delegateContinuePrompt(child, instance)
	}
	launched, err := a.launcher.LaunchDelegate(ctx, launch)
	if err != nil {
		return "", fmt.Errorf("launching the delegate for %s (round %d): %w", child, launch.Round, err)
	}
	payload, err := json.Marshal(delegateLaunchRecord{Round: launch.Round, Session: launched.Session})
	if err != nil {
		return "", err
	}
	for attempt := 0; ; attempt++ {
		_, _, stored, err = a.resolveSession(ctx, identity, child)
		if err != nil {
			return "", err
		}
		if stored.Metadata.Ended != nil {
			return launched.Session, nil
		}
		metadata := stored.Metadata
		metadata.UpdatedAt = a.now().UTC().Round(0)
		_, err = a.sessions.Append(ctx, child, stored.Version, SessionAppend{Metadata: &metadata, Events: []StoredEvent{{CodecVersion: SessionCodecVersion, Code: workflowDelegateLaunchCode, Payload: payload}}})
		var appErr *ApplicationError
		if err != nil && attempt == 0 && errors.As(err, &appErr) && appErr.Code == ErrorSessionConflict {
			continue
		}
		return launched.Session, err
	}
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
	_, err := a.abandonSession(ctx, identity, WorkflowResumeRequest{SessionID: child}, reason, false)
	var appErr *ApplicationError
	if errors.As(err, &appErr) && appErr.Code == ErrorSessionEnded {
		return nil
	}
	return err
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
