package application

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/networkteam/slogutils"

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
		w.demandProject("")
		return w.project, w.branch, w.branch != "", nil
	}
	if err := w.authorizeTarget(project, AccessRead); err != nil {
		return "", "", false, err
	}
	w.demandProject(project)
	return project, "", false, nil
}

// resolveTargetProject resolves a project an instance of a session homed in
// home may work in. Two grants, kept distinct: the target is one of the home
// project's direct dependencies (20261113-235859-d-cpt-l3m) — a graph fact,
// true for every principal — and the principal is a member of the target,
// asked with the access the operation needs. Reading a dependency inside the
// home view and being in it are different questions; this is the second.
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
	if !a.directDependencies(ctx, principal, home).holds(target) {
		return Principal{}, nil, &ApplicationError{Code: ErrorProjectUnavailable, Message: model.NotDirectDependency("project "+string(target), string(home.options.Project.ID)).Error()}
	}
	runtime, err := a.resolveProject(ctx, principal, target, required)
	if err != nil {
		return Principal{}, nil, err
	}
	return principal, runtime, nil
}

// horizon is what a read reaches beyond its project: the project's direct
// dependencies (20261113-235859-d-cpt-l3m), in declared order, each with the
// runtime the AccessResolver resolved it to for the principal, so a read
// reaches exactly what a session may work in. A declaration the composition
// refuses keeps a nil runtime, so a read selecting it fails as unavailable
// rather than silently narrowing. A member's view is acquired once per
// horizon.
type horizon struct {
	home     ProjectID
	repos    []RepoID
	runtimes map[RepoID]*ProjectRuntime
	view     func(*ProjectRuntime, string) (*materializedGraphView, error)
}

func (a *Application) directDependencies(ctx context.Context, principal Principal, home *ProjectRuntime) *horizon {
	homeID := home.options.Project.ID
	views := map[ProjectID]*materializedGraphView{}
	h := &horizon{home: homeID, runtimes: map[RepoID]*ProjectRuntime{}, view: func(runtime *ProjectRuntime, branch string) (*materializedGraphView, error) {
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
	}}
	for _, dependency := range home.options.Dependencies {
		if _, seen := h.runtimes[dependency]; seen {
			continue
		}
		h.repos = append(h.repos, dependency)
		runtime, err := a.access.ResolveDependency(ctx, principal, homeID, dependency)
		if err != nil || runtime == nil || runtime.options.Project.ID == homeID {
			h.runtimes[dependency] = nil
			continue
		}
		h.runtimes[dependency] = runtime
	}
	return h
}

// holds reports whether project is one of the resolved direct dependencies.
func (h *horizon) holds(project ProjectID) bool {
	for _, runtime := range h.runtimes {
		if runtime != nil && runtime.options.Project.ID == project {
			return true
		}
	}
	return false
}

// member returns a direct dependency's runtime, or the unavailable error for
// one the composition refused and the refusal naming why for a repo beyond
// the direct dependencies.
func (h *horizon) member(repoID RepoID) (*ProjectRuntime, error) {
	runtime, declared := h.runtimes[repoID]
	if !declared {
		return nil, &ApplicationError{Code: ErrorProjectUnavailable, Message: model.NotDirectDependency("repo "+string(repoID), string(h.home)).Error()}
	}
	if runtime == nil {
		return nil, dependencyUnavailable()
	}
	return runtime, nil
}

// demandProject starts or renews the lease of project's graph for the call
// in flight (Application.demand): the home graph on the session's branch,
// another project on its default branch. A sync round it ran drops the
// graph views read before it.
func (w *WorkflowSession) demandProject(project ProjectID) {
	branch := ""
	if project == "" || project == w.project {
		project, branch = w.project, w.branch
	}
	runtime, err := w.targetRuntime(project, AccessRead)
	if err != nil {
		slogutils.FromContext(w.ctx).Debug("sync: no lease for an unresolved project", "project", project, "err", err)
		return
	}
	if w.app.demand(w.ctx, runtime, branch) && w.graphs != nil {
		w.graphs.Invalidate()
	}
}
