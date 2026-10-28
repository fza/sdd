package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

// An instance targets a project: recorded when the start call names one,
// derived from the dispatching parent or the home project otherwise
// (d-cpt-yjc). The engine sees none of this — the fact lives in the session
// ledger as the instance-level counterpart of the branch binding.

type instanceProjectEvent struct {
	Instance string    `json:"instance"`
	Project  ProjectID `json:"project"`
}

func instanceProjectRecord(instance string, project ProjectID) (StoredEvent, error) {
	payload, err := json.Marshal(instanceProjectEvent{Instance: instance, Project: project})
	if err != nil {
		return StoredEvent{}, err
	}
	return StoredEvent{CodecVersion: SessionCodecVersion, Code: workflowInstanceProjectCode, Payload: payload}, nil
}

func (w *WorkflowSession) restoreInstanceProjects(events []StoredEvent) error {
	for _, stored := range events {
		if stored.Code != workflowInstanceProjectCode {
			continue
		}
		if !SupportedSessionCodecVersion(stored.CodecVersion) {
			return &ApplicationError{Code: ErrorMigrationRequired, Message: "unsupported instance project event codec", Version: stored.CodecVersion}
		}
		var event instanceProjectEvent
		if err := json.Unmarshal(stored.Payload, &event); err != nil {
			return fmt.Errorf("decoding instance project event: %w", err)
		}
		if event.Instance == "" || event.Project == "" {
			return fmt.Errorf("decoding instance project event: instance and project are required")
		}
		w.instanceProjects[event.Instance] = event.Project
	}
	return nil
}

// instanceProject resolves the project an instance targets: its own record,
// else the nearest recorded ancestor's, else the home project.
func (w *WorkflowSession) instanceProject(instance string) ProjectID {
	if w.session == nil {
		return w.project
	}
	for id := instance; id != ""; {
		if project, ok := w.instanceProjects[id]; ok {
			return project
		}
		inst, ok := w.session.Instance(id)
		if !ok {
			break
		}
		id = inst.Parent
	}
	return w.project
}

// Contextual graph reads receive the live instance store; commands identify
// their instance explicitly because their candidate store is a clone.
func (w *WorkflowSession) projectFor(store *engine.Store) ProjectID {
	if w.session == nil {
		return w.project
	}
	for _, inst := range w.session.Instances() {
		if inst.Store == store {
			return w.instanceProject(inst.ID)
		}
	}
	return w.project
}

// startProjectFor settles the project a start call pins, validated before the
// instance exists; empty leaves the instance to derive its project.
func (w *WorkflowSession) startProjectFor(explicit ProjectID) (ProjectID, error) {
	if explicit == "" {
		return "", nil
	}
	if err := w.authorizeTarget(explicit, AccessRead); err != nil {
		return "", err
	}
	return explicit, nil
}

// targetRuntime resolves the runtime of a project an instance targets, with
// the access the operation needs. The home project resolves directly; another
// project passes the two grants of d-cpt-yjc.
func (w *WorkflowSession) targetRuntime(project ProjectID, required Access) (*ProjectRuntime, error) {
	_, home, err := w.app.resolve(w.ctx, w.identity, w.project, AccessRead)
	if err != nil {
		return nil, err
	}
	if project == "" || project == w.project {
		if required == AccessRead {
			return home, nil
		}
		_, runtime, err := w.app.resolve(w.ctx, w.identity, w.project, required)
		return runtime, err
	}
	_, runtime, err := w.app.resolveTargetProject(w.ctx, w.identity, home, project, required, w.branch, w.graphs.sourceView)
	return runtime, err
}

// authorizeTarget is targetRuntime without the runtime — the gate a write
// passes before its intent is recorded, so a principal without write access is
// refused before anything is remembered rather than at the publication the
// application authorizes again. A read at home costs nothing: the session
// resolved the home project for reading when it opened.
func (w *WorkflowSession) authorizeTarget(project ProjectID, required Access) error {
	if (project == "" || project == w.project) && required == AccessRead {
		return nil
	}
	_, err := w.targetRuntime(project, required)
	return err
}

// ReadScope resolves where a free read runs: the home project on the session's
// branch binding by default, or another project the session may work in — on
// that project's configured default, since the binding is a fact about the
// home checkout alone.
func (w *WorkflowSession) ReadScope(ctx context.Context, identity RequestIdentity, project ProjectID) (ProjectID, string, bool, error) {
	w.setOperation(ctx, identity)
	if project == "" || project == w.project {
		return w.project, w.branch, w.branch != "", nil
	}
	if err := w.authorizeTarget(project, AccessRead); err != nil {
		return "", "", false, err
	}
	return project, "", false, nil
}

// resolveTargetProject resolves a project an instance of a session homed in
// home may work in. Two grants, kept distinct: the target lies in the home
// project's declared dependency closure — a graph fact, true for every
// principal — and the principal is a member of the target, asked with the
// access the operation needs. Reading a dependency inside the home view and
// being in it are different questions; this is the second.
func (a *Application) resolveTargetProject(ctx context.Context, identity RequestIdentity, home *ProjectRuntime, target ProjectID, required Access, homeBranch string, sourceView func(*ProjectRuntime, string) (*materializedGraphView, error)) (Principal, *ProjectRuntime, error) {
	principal, err := a.resolvePrincipal(ctx, identity)
	if err != nil {
		return Principal{}, nil, err
	}
	if target == "" || target == home.options.Project.ID {
		runtime, err := a.resolveProject(ctx, principal, home.options.Project.ID, required)
		return principal, runtime, err
	}
	if required == AccessRead {
		selected, err := sourceView(home, homeBranch)
		if err != nil {
			return Principal{}, nil, err
		}
		home = selected.runtime
	}
	if err := a.inDependencyClosure(ctx, principal, home, target, required == AccessRead, sourceView); err != nil {
		return Principal{}, nil, err
	}
	runtime, err := a.resolveProject(ctx, principal, target, required)
	if err != nil {
		return Principal{}, nil, err
	}
	return principal, runtime, nil
}

// inDependencyClosure walks the declared dependencies transitively from home
// through the configurations the composition can reach (model.DependencyClosure).
// Membership is a property of the resolved project, never of the declared repo
// ID: only the composition knows which project carries it.
func (a *Application) inDependencyClosure(ctx context.Context, principal Principal, home *ProjectRuntime, target ProjectID, readConfig bool, sourceView func(*ProjectRuntime, string) (*materializedGraphView, error)) error {
	homeID := home.options.Project.ID
	for id, err := range model.DependencyClosure(ctx, homeID, a.closureStep(principal, home, readConfig, sourceView)) {
		if err != nil {
			return err
		}
		if id == target {
			return nil
		}
	}
	return &ApplicationError{Code: ErrorProjectUnavailable, Message: fmt.Sprintf("project %s is not in the declared dependency closure of %s", target, homeID)}
}

func (a *Application) closureStep(principal Principal, home *ProjectRuntime, readConfig bool, sourceView func(*ProjectRuntime, string) (*materializedGraphView, error)) *accessDependencies {
	homeID := home.options.Project.ID
	return &accessDependencies{
		access: a.access, principal: principal, home: homeID, readConfig: readConfig, sourceView: sourceView,
		runtimes: map[ProjectID]*ProjectRuntime{homeID: home}, byRepo: map[RepoID]*ProjectRuntime{},
	}
}

// horizon is the dependency closure a read reaches (20261026-194528-d-cpt-gcq):
// the repos the walk reached, in walk order, each with the runtime it resolved
// to, read through the same step as the gate so a read reaches exactly what a
// session may work in. A member's view is acquired once per horizon.
type horizon struct {
	repos    []RepoID
	runtimes map[RepoID]*ProjectRuntime
	view     func(*ProjectRuntime, string) (*materializedGraphView, error)
}

func (a *Application) horizon(ctx context.Context, principal Principal, home *ProjectRuntime) (*horizon, error) {
	views := map[ProjectID]*materializedGraphView{}
	view := func(runtime *ProjectRuntime, branch string) (*materializedGraphView, error) {
		if v := views[runtime.options.Project.ID]; v != nil {
			return v, nil
		}
		snapshot, selected, err := readMaterializedSnapshot(ctx, runtime, branch)
		if err != nil {
			return nil, err
		}
		v := &materializedGraphView{snapshot: snapshot, runtime: selected}
		views[runtime.options.Project.ID] = v
		return v, nil
	}
	step := a.closureStep(principal, home, true, view)
	// A member whose source view cannot be read declares nothing, so it fails
	// only a read that selects it, which reads it again
	// (20260903-153224-s-cpt-ir1).
	readable := model.DependencyResolverFunc[ProjectID](func(ctx context.Context, id ProjectID) ([]ProjectID, error) {
		dependencies, err := step.Dependencies(ctx, id)
		var cleanup *snapshotReleaseError
		if err != nil && ctx.Err() == nil && !errors.As(err, &cleanup) {
			return nil, nil
		}
		return dependencies, err
	})
	for _, err := range model.DependencyClosure(ctx, home.options.Project.ID, readable) {
		if err != nil {
			return nil, err
		}
	}
	return &horizon{repos: step.repos, runtimes: step.byRepo, view: view}, nil
}

// member returns a closure member's runtime, or the unavailable error for a
// repo outside the closure or one the composition refused.
func (h *horizon) member(repoID RepoID) (*ProjectRuntime, error) {
	if runtime := h.runtimes[repoID]; runtime != nil {
		return runtime, nil
	}
	return nil, dependencyUnavailable()
}

// accessDependencies is serve's step of the closure walk, built per walk. A
// project's declarations resolve per principal and per declaring project
// through the AccessResolver; a dependency the composition cannot resolve is
// not part of the reachable closure, so nothing behind it is a valid target.
// The runtimes it resolved are kept so the next step reads their
// declarations, and with readConfig a dependency's declarations come from its
// selected source view, as a read sees them. byRepo and repos keep the first
// runtime each declared repo ID resolved to, for reads that select by repo ID.
type accessDependencies struct {
	access     AccessResolver
	principal  Principal
	home       ProjectID
	readConfig bool
	sourceView func(*ProjectRuntime, string) (*materializedGraphView, error)
	runtimes   map[ProjectID]*ProjectRuntime
	byRepo     map[RepoID]*ProjectRuntime
	repos      []RepoID
}

func (d *accessDependencies) Dependencies(ctx context.Context, id ProjectID) ([]ProjectID, error) {
	current := d.runtimes[id]
	if d.readConfig && id != d.home {
		selected, err := d.sourceView(current, "")
		if err != nil {
			return nil, err
		}
		current = selected.runtime
	}
	var resolved []ProjectID
	for _, dependency := range current.options.Dependencies {
		runtime, err := d.access.ResolveDependency(ctx, d.principal, current.options.Project.ID, dependency)
		if err != nil || runtime == nil {
			d.record(dependency, nil)
			continue
		}
		dependencyID := runtime.options.Project.ID
		if _, known := d.runtimes[dependencyID]; !known {
			d.runtimes[dependencyID] = runtime
		}
		if dependencyID != d.home {
			d.record(dependency, runtime)
		}
		resolved = append(resolved, dependencyID)
	}
	return resolved, nil
}

// record keeps the first runtime a declared repo ID resolved to; a nil
// runtime keeps a declaration the composition refused, so a read selecting
// it fails as unavailable rather than silently narrowing.
func (d *accessDependencies) record(repoID RepoID, runtime *ProjectRuntime) {
	known, seen := d.byRepo[repoID]
	if !seen {
		d.repos = append(d.repos, repoID)
	}
	if known == nil {
		d.byRepo[repoID] = runtime
	}
}
