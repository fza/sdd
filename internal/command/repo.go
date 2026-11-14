package command

import (
	"fmt"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

// RepoAddCmd connects a repo with two writes: the committed dependency
// declaration in the current repo's .sdd/config.yaml (what this graph
// needs — portable, go.mod-style) and the per-user resolution
// {repo_id, clone_url} in the user-global config (how this machine reaches
// it). Re-running on an existing connection is the upgrade path that adds a
// missing declaration.
type RepoAddCmd struct {
	// CloneURL is the git URL to clone from (ssh or https — per-user
	// choice; the derived repo_id is identical either way).
	CloneURL string

	// OnAdded fires after the connection is registered, carrying the
	// verified repo identity and where its sdd-owned clone lives.
	OnAdded func(repoID model.RepoID, cloneDir string)

	// OnDeclared fires when the dependency declaration is ensured in the
	// current repo's committed config; alreadyDeclared reports whether it
	// was present before. Not fired outside an sdd repo (global-only
	// registration is legitimate — resolution without a dependent graph).
	OnDeclared func(repoID model.RepoID, alreadyDeclared bool)

	// OnPhase reports the active stage (connecting → cloning) so the footer
	// shows real progress rather than a bare eternal "connecting" spinner.
	// Optional; fired only on the clone path, never the already-connected no-op.
	OnPhase func(phase model.Phase)
}

// ResolveFurtherRefCmd makes the entry a direct dependency's ref names in a
// repo beyond the local repo's direct dependencies available in that repo's
// clone (20261113-235859-d-cpt-l3m): read from the clone as it stands, else
// one fetch on a miss through the git gate, which may spend a credit inside
// the fetch cooldown and is waited for at most read_wait.
type ResolveFurtherRefCmd struct {
	RepoID  model.RepoID
	EntryID string

	// Fetched reports that a fetch of RepoID completed earlier in the same
	// read: a miss then is recorded without another fetch.
	Fetched bool

	// OnFetched reports that this command completed a fetch. Optional.
	OnFetched func()

	// OnUnavailable reports why the entry stays unavailable. Required.
	OnUnavailable func(ref model.UnavailableRef)
}

// Validate checks that no unavailable entry goes unreported.
func (c *ResolveFurtherRefCmd) Validate() error {
	if c.RepoID == "" || c.EntryID == "" {
		return fmt.Errorf("repo ID and entry ID are required")
	}
	if c.OnUnavailable == nil {
		return fmt.Errorf("OnUnavailable is required")
	}
	return nil
}

// EnsureReposFreshCmd starts or renews the leases of the named connected
// repos for a read: a clone on first use, else the sync round a lease start
// asks for. OnPhase reports connecting → cloning for a clone and syncing for
// a round, so a text-only cross-repo read shows a phase-true footer; it is
// never fired for a round not due.
type EnsureReposFreshCmd struct {
	RepoIDs []model.RepoID

	// OnPhase reports the freshening stage. Optional.
	OnPhase func(phase model.Phase)

	// OnPending reports a round still running past the read wait, with when
	// the clone last fetched; OnSettled that round's outcome once it ends.
	// Optional.
	OnPending func(repoID model.RepoID, fetched time.Time)
	OnSettled func(outcome model.RepoSyncOutcome)
}

// RefreshDependenciesCmd refreshes the clones of the local repo's direct
// dependencies (20261113-235859-d-cpt-l3m) ahead of a read. The refresh is
// optional to the read (20261027-150208-d-tac-bg4): its failures are reported
// through the callbacks, and the read uses the clones as they stand.
type RefreshDependenciesCmd struct {
	// RepoIDs narrows the refresh to these direct dependencies; all of them
	// when empty.
	RepoIDs []model.RepoID

	// OnRefreshFailed reports a clone or sync round that failed. Required.
	OnRefreshFailed func(repoID model.RepoID, err error)

	// OnUnconnected reports a direct dependency with no connection on this
	// machine. Required.
	OnUnconnected func(repoID model.RepoID)

	// OnPhase reports the refresh stage. Optional.
	OnPhase func(phase model.Phase)

	// OnPending reports a sync round still running past the read wait, with
	// when the clone last fetched. Optional.
	OnPending func(repoID model.RepoID, fetched time.Time)
}

// Validate checks that no refresh failure can go unreported.
func (c *RefreshDependenciesCmd) Validate() error {
	if c.OnRefreshFailed == nil || c.OnUnconnected == nil {
		return fmt.Errorf("OnRefreshFailed and OnUnconnected are required")
	}
	return nil
}

// Validate checks the command's required fields.
func (c *RepoAddCmd) Validate() error {
	if c.CloneURL == "" {
		return fmt.Errorf("clone URL is required")
	}
	return nil
}

// RepoRemoveCmd drops a declared cross-repo dependency from the current
// repo's committed .sdd/config.yaml and commits that change. It is
// project-scoped and reference-safety-guarded: because entries are immutable,
// a referenced dependency is permanent, so removal is refused when any entry
// in this graph still holds a cross-repo ref into the target — dropping the
// declaration would strand those refs and retroactively break resolve-or-block.
// The per-user global connection and its sdd-owned clone are left untouched;
// machine-level teardown is a separate, future command surface.
type RepoRemoveCmd struct {
	RepoID model.RepoID

	// Force drops the dependency even when local entries still reference the
	// target, stranding those refs. Required to proceed past the ref-safety
	// guard; the stranded refs are named through OnStranded, never silently.
	Force bool

	// OnRemoved fires after the dependency is dropped from the committed
	// config and the change is committed.
	OnRemoved func(repoID model.RepoID)

	// OnStranded fires when Force drops a still-referenced dependency,
	// carrying the refs the removal stranded so the override stays loud.
	OnStranded func(repoID model.RepoID, stranded []StrandedRef)
}

// StrandedRef names a local entry whose cross-repo reference into a removed
// dependency is left dangling by a forced `sdd repo remove`.
type StrandedRef struct {
	// EntryID is the local entry that holds the reference.
	EntryID string
	// RefID is the full cross-repo ref it points at (repo-id:entry-id).
	RefID string
	// Kind is the reference kind (e.g. builds-on, grounded-in).
	Kind string
}

// Validate checks the command's required fields.
func (c *RepoRemoveCmd) Validate() error {
	if c.RepoID == "" {
		return fmt.Errorf("repo ID is required")
	}
	return nil
}

// RepoSyncCmd runs the sync round of connected repos through their
// sdd-owned clones (20261107-095427-d-tac-yux): the named repos, or the
// local repo's direct dependencies when RepoIDs is empty, every connected
// repo outside a project. Inside a project, a named repo outside its direct
// dependencies is refused (20261113-235859-d-cpt-l3m). A repo without a clone
// is cloned.
type RepoSyncCmd struct {
	RepoIDs []model.RepoID

	// Due selects instead every connected direct dependency of the local repo
	// that has a clone, each running the timer round its lease asks for
	// (CloneRound.TimerRound): no clone is made, and a round
	// another process is running is left to it.
	Due bool

	// OnOutcome receives each repo's outcome, in the order the repos were
	// named or the local repo declares them. Required.
	OnOutcome func(outcome model.RepoSyncOutcome)
}

// Validate checks that no outcome goes unreported.
func (c *RepoSyncCmd) Validate() error {
	if c.OnOutcome == nil {
		return fmt.Errorf("OnOutcome is required")
	}
	return nil
}
