package cliapp

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
	sdd "github.com/networkteam/sdd/pkg/application"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

// pendingGraphClone is the first clone of the served project's graph
// repository. sdd serve makes it after the MCP handshake rather than before,
// which an agent harness's startup deadline cuts short.
type pendingGraphClone struct {
	repoID model.RepoID
	clone  func(context.Context) error

	mu   sync.Mutex
	done bool
	err  error
}

// withPendingGraphClone serves the pending first graph clone instead of
// action, which would make it before the handshake.
func withPendingGraphClone(action cli.ActionFunc) cli.ActionFunc {
	return func(ctx context.Context, cmd *cli.Command) error {
		if cmd.String("graph-dir") != "" {
			return action(ctx, cmd)
		}
		pending, err := firstGraphClone()
		if err != nil {
			return err
		}
		if pending == nil {
			return action(ctx, cmd)
		}
		return servePendingGraphClone(ctx, cmd, pending)
	}
}

// firstGraphClone returns the pending first clone of the working directory's
// graph repository; nil when the project has no graph_repo, or its clone
// stands.
func firstGraphClone() (*pendingGraphClone, error) {
	project, clones, err := discoverProject()
	if errors.Is(err, errNoProject) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if project.GraphRepo == "" {
		return nil, nil
	}
	dir, err := clones.Dir(project.GraphRepo)
	if err != nil {
		return nil, err
	}
	if repos.IsCloned(dir) {
		return nil, nil
	}
	return &pendingGraphClone{
		repoID: project.GraphRepo,
		clone:  func(ctx context.Context) error { return ensureGraphClone(ctx, clones, project) },
	}, nil
}

func (p *pendingGraphClone) run(ctx context.Context) {
	err := p.clone(ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.done, p.err = true, err
}

// unavailable is what every call gets while the server serves the pending
// clone: its state and the fix.
func (p *pendingGraphClone) unavailable() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var message string
	switch {
	case !p.done:
		message = fmt.Sprintf("graph repo %s is cloning on this machine; restart the sdd server once it is cloned, or run `sdd repo sync %s` in a terminal to watch the clone", p.repoID, p.repoID)
	case p.err != nil:
		message = fmt.Sprintf("graph repo %s could not be cloned: %v; run `sdd repo sync %s`, then restart the sdd server", p.repoID, p.err, p.repoID)
	default:
		message = fmt.Sprintf("graph repo %s is cloned now; restart the sdd server to serve it", p.repoID)
	}
	return &sdd.ApplicationError{Code: sdd.ErrorProjectUnavailable, Message: message, Cause: p.err}
}

// servePendingGraphClone answers the MCP handshake at once and every call
// with the clone's state while the clone runs.
func servePendingGraphClone(ctx context.Context, cmd *cli.Command, pending *pendingGraphClone) error {
	locations, err := repos.DefaultLocations()
	if err != nil {
		return err
	}
	// The pending access refuses every call before it reaches a store, so
	// nothing is written here.
	unused := filepath.Join(locations.StateRoot, "pending-graph-clone")
	sessions, err := localadapter.NewFilesystemSessionStoreAt(filepath.Join(unused, "sessions"))
	if err != nil {
		return err
	}
	blobs, err := localadapter.NewFilesystemStagedBlobStoreAt(filepath.Join(unused, "staged"))
	if err != nil {
		return err
	}
	application, err := sdd.NewApplication(sdd.ApplicationOptions{Access: pendingAccess{pending}, Sessions: sessions, StagedBlobs: blobs})
	if err != nil {
		return err
	}
	cloneCtx, stopClone := context.WithCancel(ctx)
	cloned := make(chan struct{})
	go func() {
		defer close(cloned)
		pending.run(cloneCtx)
	}()
	defer func() {
		stopClone()
		<-cloned
	}()
	return runMCPServer(ctx, cmd, application, sdd.RequestIdentity{Subject: "local"})
}

// pendingAccess admits the local principal and refuses every project with
// the pending clone's state.
type pendingAccess struct{ pending *pendingGraphClone }

func (a pendingAccess) ResolvePrincipal(_ context.Context, identity sdd.RequestIdentity) (sdd.Principal, error) {
	return sdd.Principal{Subject: identity.Subject}, nil
}

func (a pendingAccess) ResolveParticipant(context.Context, sdd.Principal, sdd.ProjectID) (string, error) {
	return "", a.pending.unavailable()
}

func (a pendingAccess) ListProjects(context.Context, sdd.Principal) (sdd.ProjectList, error) {
	return sdd.ProjectList{}, a.pending.unavailable()
}

func (a pendingAccess) ResolveProject(context.Context, sdd.Principal, sdd.ProjectID, sdd.Access) (*sdd.ProjectRuntime, error) {
	return nil, a.pending.unavailable()
}

func (a pendingAccess) ResolveDependency(context.Context, sdd.Principal, sdd.ProjectID, sdd.RepoID) (*sdd.ProjectRuntime, error) {
	return nil, a.pending.unavailable()
}

func (a pendingAccess) AuthorizeSession(context.Context, sdd.SessionAccessRequest) error {
	return a.pending.unavailable()
}
