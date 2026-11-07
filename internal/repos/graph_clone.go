package repos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"

	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

// GraphCloneGit is the git surface the sdd-owned graph clones need.
type GraphCloneGit interface {
	CloneGraph(ctx context.Context, repoID model.RepoID, source model.CloneSource, dir string, cone []string) error
	SparseCone(ctx context.Context, dir string) ([]string, error)
	SetSparseCone(ctx context.Context, dir string, cone []string) error
	KeepGraphClone(ctx context.Context, dir string, routing []string) error
	PushGraphClone(ctx context.Context, dir string) error
	// RemoteURL is the URL of the protocol remote the clone at dir uses.
	RemoteURL(ctx context.Context, dir string) (string, error)
	// ExpandURL applies git's URL rewrites to url without reaching it.
	ExpandURL(ctx context.Context, url string) (string, error)
}

// GraphClones owns the sdd-owned clone of every graph and connected
// repository this machine reads or writes, one per repository id under
// <DataRoot>/repos/<repo-id>/ (20261106-201216-d-cpt-aim). A clone passes
// the gate as a fetch, a publish as a push (20261107-095427-d-tac-yux).
type GraphClones struct {
	dataRoot string
	git      GraphCloneGit
	gate     *gitgate.Gate
}

// NewGraphClones builds the clone owner over the locations' data root. A nil
// gate serves only the clones' places; a clone or publish fails with
// ErrNoGate.
func NewGraphClones(loc Locations, git GraphCloneGit, gate *gitgate.Gate) *GraphClones {
	return &GraphClones{dataRoot: loc.DataRoot, git: git, gate: gate}
}

// Dir is where the clone of repoID lives.
func (g *GraphClones) Dir(repoID model.RepoID) (string, error) {
	return cloneDir(g.dataRoot, repoID)
}

// Ensure clones the graph repository repoID when no clone exists yet, trying
// the protocol codeOrigin suggests first, then keeps it (see keep). It
// reports whether a clone ran.
func (g *GraphClones) Ensure(ctx context.Context, repoID model.RepoID, codeOrigin string) (dir string, cloned bool, err error) {
	return g.ensure(ctx, repoID, model.CloneSource{First: model.InitialProtocol(repoID, codeOrigin)})
}

// EnsureConnected clones the connected repository repoID from cloneURL when
// no clone exists yet (model.ConnectedCloneSource), then keeps it.
func (g *GraphClones) EnsureConnected(ctx context.Context, repoID model.RepoID, cloneURL string) (dir string, cloned bool, err error) {
	return g.ensure(ctx, repoID, model.ConnectedCloneSource(repoID, cloneURL))
}

func (g *GraphClones) ensure(ctx context.Context, repoID model.RepoID, source model.CloneSource) (dir string, cloned bool, err error) {
	dir, err = g.Dir(repoID)
	if err != nil {
		return "", false, err
	}
	if !IsCloned(dir) {
		err = g.clone(ctx, repoID, source, dir)
		if err != nil {
			return "", false, err
		}
		cloned = true
	}
	err = g.keep(ctx, repoID, dir)
	if err != nil {
		return "", false, err
	}
	return dir, cloned, nil
}

// Stage clones cloneURL into a fresh directory beside the clones, for a
// repository whose id only its committed config declares. remove deletes
// what Place did not move.
func (g *GraphClones) Stage(ctx context.Context, cloneURL string) (staged string, remove func(), err error) {
	root := filepath.Join(g.dataRoot, "repos")
	err = os.MkdirAll(root, 0o755)
	if err != nil {
		return "", nil, fmt.Errorf("creating %s: %w", root, err)
	}
	parent, err := os.MkdirTemp(root, ".adding-*")
	if err != nil {
		return "", nil, err
	}
	remove = func() { _ = os.RemoveAll(parent) }
	derived, deriveErr := model.DeriveRepoID(cloneURL)
	source := model.CloneSource{URL: cloneURL}
	if deriveErr == nil {
		source = model.ConnectedCloneSource(derived, cloneURL)
	} else {
		derived = ""
	}
	staged = filepath.Join(parent, "clone")
	err = g.clone(ctx, derived, source, staged)
	if err != nil {
		remove()
		return "", nil, err
	}
	return staged, remove, nil
}

// Place moves a staged clone to where the clone of repoID lives and keeps
// it; a clone already there stays and the staged one is left to its remove.
func (g *GraphClones) Place(ctx context.Context, staged string, repoID model.RepoID) (string, error) {
	dir, err := g.Dir(repoID)
	if err != nil {
		return "", err
	}
	if !IsCloned(dir) {
		err = os.MkdirAll(filepath.Dir(dir), 0o755)
		if err != nil {
			return "", fmt.Errorf("creating %s: %w", filepath.Dir(dir), err)
		}
		err = os.Rename(staged, dir)
		if err != nil {
			return "", fmt.Errorf("placing the clone of %s: %w", repoID, err)
		}
	}
	return dir, g.keep(ctx, repoID, dir)
}

// keep restores the clone at dir to what sdd made: the cone holds .sdd and
// the graph_dir its committed config names, merges route to sdd's drivers,
// and anything sdd did not make is gone.
func (g *GraphClones) keep(ctx context.Context, repoID model.RepoID, dir string) error {
	err := g.git.KeepGraphClone(ctx, dir, nil)
	if err != nil {
		return fmt.Errorf("keeping the clone of %s: %w", repoID, err)
	}
	graphDir, err := committedGraphDir(dir)
	if err != nil {
		return err
	}
	cone := model.GraphCloneCone(graphDir)
	current, err := g.git.SparseCone(ctx, dir)
	if err != nil {
		return err
	}
	if !slices.Equal(current, cone) {
		err = g.git.SetSparseCone(ctx, dir, cone)
		if err != nil {
			return err
		}
	}
	entryRouting, err := model.EntryMergeAttributeLine(graphDir)
	if err != nil {
		return fmt.Errorf("graph_dir of %s: %w", repoID, err)
	}
	err = g.git.KeepGraphClone(ctx, dir, []string{entryRouting, model.ConfigMergeAttributeLine})
	if err != nil {
		return fmt.Errorf("keeping the clone of %s: %w", repoID, err)
	}
	return nil
}

// Publish pushes what sdd committed in the clone of repoID.
func (g *GraphClones) Publish(ctx context.Context, repoID model.RepoID) error {
	dir, err := g.Dir(repoID)
	if err != nil {
		return err
	}
	err = g.run(ctx, gitgate.Push, func(ctx context.Context) (string, error) { return g.git.RemoteURL(ctx, dir) }, func(ctx context.Context) error {
		return g.git.PushGraphClone(ctx, dir)
	})
	if err != nil {
		return fmt.Errorf("publishing the clone of %s: %w", repoID, err)
	}
	return nil
}

// Target is the remote a fetch of repoID reaches: its clone's remote, or the
// one its first clone from cloneURL tries first.
func (g *GraphClones) Target(ctx context.Context, repoID model.RepoID, cloneURL string) (gitgate.Target, error) {
	dir, err := g.Dir(repoID)
	if err != nil {
		return gitgate.Target{}, err
	}
	var url string
	if IsCloned(dir) {
		url, err = g.git.RemoteURL(ctx, dir)
	} else {
		url, err = g.sourceURL(ctx, repoID, model.ConnectedCloneSource(repoID, cloneURL))
	}
	if err != nil {
		return gitgate.Target{}, err
	}
	return gitgate.TargetFromURL(url)
}

// clone makes the clone of repoID at dir from source, its target the remote
// tried first.
func (g *GraphClones) clone(ctx context.Context, repoID model.RepoID, source model.CloneSource, dir string) error {
	return g.run(ctx, gitgate.Fetch, func(ctx context.Context) (string, error) { return g.sourceURL(ctx, repoID, source) }, func(ctx context.Context) error {
		return g.git.CloneGraph(ctx, repoID, source, dir, model.GraphCloneCone(""))
	})
}

// sourceURL is the URL a clone from source reaches first, rewrites applied.
func (g *GraphClones) sourceURL(ctx context.Context, repoID model.RepoID, source model.CloneSource) (string, error) {
	url := source.URL
	if url == "" {
		sshURL, httpsURL, err := model.GraphRemoteURLs(repoID)
		if err != nil {
			return "", err
		}
		url = sshURL
		if source.First == model.ProtocolHTTPS {
			url = httpsURL
		}
	}
	return g.git.ExpandURL(ctx, url)
}

func (g *GraphClones) run(ctx context.Context, kind gitgate.Kind, remoteURL func(ctx context.Context) (string, error), op func(ctx context.Context) error) error {
	if g.gate == nil {
		return ErrNoGate
	}
	url, err := remoteURL(ctx)
	if err != nil {
		return err
	}
	target, err := gitgate.TargetFromURL(url)
	if err != nil {
		return err
	}
	return g.gate.Run(ctx, target, kind, op)
}

// committedGraphDir is the graph directory the clone's committed config
// names, relative to its root; the default for a repository without one.
func committedGraphDir(dir string) (string, error) {
	cfg, err := meta.ReadCommittedConfigFS(os.DirFS(dir))
	if errors.Is(err, meta.ErrNotAnSDDProject) {
		return model.DefaultGraphDir, nil
	}
	if err != nil {
		return "", err
	}
	if cfg.GraphDir == "" || cfg.GraphDir == model.GraphElsewhereFile {
		return model.DefaultGraphDir, nil
	}
	return path.Clean(filepath.ToSlash(cfg.GraphDir)), nil
}
