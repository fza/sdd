package repos_test

import (
	"context"
	"path/filepath"
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
