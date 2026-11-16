package repos_test

import (
	"context"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/test"
)

// heldClone clones through git and holds the first clone until release
// closes.
type heldClone struct {
	git.GraphClones
	clones  atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (h *heldClone) CloneGraph(ctx context.Context, repoID model.RepoID, source model.CloneSource, dir string, cone []string) error {
	if h.clones.Add(1) == 1 {
		close(h.started)
		<-h.release
	}
	return h.GraphClones.CloneGraph(ctx, repoID, source, dir, cone)
}

// A second process asking for a clone another is making waits for it rather
// than clearing its staging and cloning again.
func TestGraphClonesEnsureWaitsForAClone(t *testing.T) {
	remote := test.NewGraphRemote(t, "example.test/acme/app-sdd")
	remote.Commit(t, "seed", map[string]string{".sdd/config.yaml": "graph_dir: .sdd/graph\n", ".sdd/graph/2026/11/07-120000-s-tac-abc.md": "---\ntype: signal\n---\nx\n"})
	locations := repos.Locations{DataRoot: filepath.Join(t.TempDir(), "data")}
	held := &heldClone{started: make(chan struct{}), release: make(chan struct{})}
	gate := gitgate.New(gitgate.Options{StateDir: t.TempDir(), Limits: func(string) gitgate.Limits { return model.DefaultGitLimits() }})
	first := repos.NewGraphClones(locations, held, gate)
	second := repos.NewGraphClones(locations, held, gate)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Go(func() { _, _, errs[0] = first.Ensure(t.Context(), remote.RepoID, "") })
	<-held.started
	wg.Go(func() { _, _, errs[1] = second.Ensure(t.Context(), remote.RepoID, "") })
	deadline := time.Now().Add(500 * time.Millisecond)
	for held.clones.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	close(held.release)
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Errorf("both callers should get the clone, got %v", err)
		}
	}
	if n := held.clones.Load(); n != 1 {
		t.Errorf("one clone should serve both callers, %d ran", n)
	}
}

// heldKeep keeps through git and, once armed, holds the next keep until
// release closes; it notes every cone change.
type heldKeep struct {
	git.GraphClones
	armed   atomic.Bool
	widened atomic.Bool
	started chan struct{}
	release chan struct{}
}

func (h *heldKeep) KeepGraphClone(ctx context.Context, dir string, routing []string) error {
	if h.armed.CompareAndSwap(true, false) {
		close(h.started)
		<-h.release
	}
	return h.GraphClones.KeepGraphClone(ctx, dir, routing)
}

func (h *heldKeep) SetSparseCone(ctx context.Context, dir string, cone []string) error {
	h.widened.Store(true)
	return h.GraphClones.SetSparseCone(ctx, dir, cone)
}

// Widen changes the cone only under the clone lock, never beside another
// process keeping the same clone.
func TestGraphClonesWidenWaitsForAKeep(t *testing.T) {
	remote := test.NewGraphRemote(t, "example.test/acme/app-sdd")
	remote.Commit(t, "seed", map[string]string{".sdd/config.yaml": "graph_dir: .sdd/graph\n", ".sdd/graph/2026/11/07-120000-s-tac-abc.md": "---\ntype: signal\n---\nx\n"})
	locations := repos.Locations{DataRoot: filepath.Join(t.TempDir(), "data")}
	held := &heldKeep{started: make(chan struct{}), release: make(chan struct{})}
	limits := func(string) gitgate.Limits { return model.DefaultGitLimits() }
	keeper := repos.NewGraphClones(locations, held, gitgate.New(gitgate.Options{StateDir: t.TempDir(), Limits: limits}))
	widener := repos.NewGraphClones(locations, held, gitgate.New(gitgate.Options{StateDir: t.TempDir(), Limits: limits}))
	dir, _, err := keeper.Ensure(t.Context(), remote.RepoID, "")
	if err != nil {
		t.Fatal(err)
	}
	held.widened.Store(false)
	held.armed.Store(true)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Go(func() { _, _, errs[0] = keeper.Ensure(t.Context(), remote.RepoID, "") })
	<-held.started
	wg.Go(func() { errs[1] = widener.Widen(t.Context(), remote.RepoID, "decisions") })
	time.Sleep(300 * time.Millisecond)
	widenedDuringKeep := held.widened.Load()
	close(held.release)
	wg.Wait()
	if widenedDuringKeep {
		t.Error("Widen changed the cone while another process kept the clone")
	}
	for _, err := range errs {
		if err != nil {
			t.Errorf("both callers should succeed, got %v", err)
		}
	}
	cone, err := git.GraphClones{}.SparseCone(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(cone, "decisions") {
		t.Errorf("the cone should hold the widened graph directory after the keep, got %v", cone)
	}
}
