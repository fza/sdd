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
	CloneGraph(ctx context.Context, repoID model.RepoID, first, dir string, cone []string) error
	SparseCone(ctx context.Context, dir string) ([]string, error)
	SetSparseCone(ctx context.Context, dir string, cone []string) error
	KeepGraphClone(ctx context.Context, dir string, routing []string) error
	PushGraphClone(ctx context.Context, dir string) error
	// RemoteURL is the URL of the protocol remote the clone at dir uses.
	RemoteURL(ctx context.Context, dir string) (string, error)
	// ExpandURL applies git's URL rewrites to url without reaching it.
	ExpandURL(ctx context.Context, url string) (string, error)
}

// GraphClones owns the sdd-owned clone of every graph repository this
// machine reads or writes, one per repository id under
// <DataRoot>/repos/<repo-id>/ (20261106-201216-d-cpt-aim). A clone passes
// the gate as a fetch, a publish as a push (20261107-095427-d-tac-yux).
type GraphClones struct {
	root string
	git  GraphCloneGit
	gate *gitgate.Gate
}

// NewGraphClones builds the clone owner over the locations' data root. A nil
// gate serves only the clones' places; a clone or publish fails with
// ErrNoGate.
func NewGraphClones(loc Locations, git GraphCloneGit, gate *gitgate.Gate) *GraphClones {
	return &GraphClones{root: filepath.Join(loc.DataRoot, "repos"), git: git, gate: gate}
}

// Dir is where the clone of repoID lives.
func (g *GraphClones) Dir(repoID model.RepoID) (string, error) {
	err := model.ValidateRepoID(repoID)
	if err != nil {
		return "", err
	}
	return filepath.Join(g.root, filepath.FromSlash(string(repoID))), nil
}

// Ensure clones repoID when no clone exists yet, trying the protocol
// codeOrigin suggests first, then keeps it: the cone holds .sdd and the
// graph_dir its committed config names, merges route to sdd's drivers, and
// anything sdd did not make is gone. It reports whether a clone ran.
func (g *GraphClones) Ensure(ctx context.Context, repoID model.RepoID, codeOrigin string) (dir string, cloned bool, err error) {
	dir, err = g.Dir(repoID)
	if err != nil {
		return "", false, err
	}
	if !IsCloned(dir) {
		err = g.clone(ctx, repoID, model.InitialProtocol(repoID, codeOrigin), dir)
		if err != nil {
			return "", false, err
		}
		cloned = true
	}
	err = g.git.KeepGraphClone(ctx, dir, nil)
	if err != nil {
		return "", false, fmt.Errorf("keeping the clone of %s: %w", repoID, err)
	}
	graphDir, err := committedGraphDir(dir)
	if err != nil {
		return "", false, err
	}
	cone := model.GraphCloneCone(graphDir)
	current, err := g.git.SparseCone(ctx, dir)
	if err != nil {
		return "", false, err
	}
	if !slices.Equal(current, cone) {
		err = g.git.SetSparseCone(ctx, dir, cone)
		if err != nil {
			return "", false, err
		}
	}
	entryRouting, err := model.EntryMergeAttributeLine(graphDir)
	if err != nil {
		return "", false, fmt.Errorf("graph_dir of %s: %w", repoID, err)
	}
	err = g.git.KeepGraphClone(ctx, dir, []string{entryRouting, model.ConfigMergeAttributeLine})
	if err != nil {
		return "", false, fmt.Errorf("keeping the clone of %s: %w", repoID, err)
	}
	return dir, cloned, nil
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

// clone makes the clone of repoID at dir, its target the remote of the
// protocol tried first.
func (g *GraphClones) clone(ctx context.Context, repoID model.RepoID, first, dir string) error {
	sshURL, httpsURL, err := model.GraphRemoteURLs(repoID)
	if err != nil {
		return err
	}
	firstURL := sshURL
	if first == model.ProtocolHTTPS {
		firstURL = httpsURL
	}
	return g.run(ctx, gitgate.Fetch, func(ctx context.Context) (string, error) { return g.git.ExpandURL(ctx, firstURL) }, func(ctx context.Context) error {
		return g.git.CloneGraph(ctx, repoID, first, dir, model.GraphCloneCone(""))
	})
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
