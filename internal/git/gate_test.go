package git_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/model"
)

func testGate(t *testing.T) *gitgate.Gate {
	t.Helper()
	return gitgate.New(gitgate.Options{StateDir: t.TempDir(), Limits: func(string) gitgate.Limits { return model.DefaultGitLimits() }})
}

// admitted returns ctx carrying the admission the gate gives a local remote,
// which holds no trace file and so outlives the Run that made it.
func admitted(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	var inside context.Context
	err := testGate(t).Run(ctx, gitgate.Target{Repo: "local", Protocol: gitgate.Local}, gitgate.Fetch, func(ctx context.Context) error {
		inside = ctx
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return inside
}

// trackedRepo is a clone of a bare remote holding one commit, on a branch
// tracking it.
func trackedRepo(t *testing.T) (dir, remote string) {
	t.Helper()
	base := t.TempDir()
	remote = filepath.Join(base, "remote.git")
	dir = filepath.Join(base, "work")
	gitRun(t, base, "init", "-q", "--bare", remote)
	gitRun(t, base, "clone", "-q", remote, dir)
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "Test")
	gitRun(t, dir, "commit", "-q", "--allow-empty", "-m", "seed")
	gitRun(t, dir, "push", "-q", "-u", "origin", "HEAD")
	return dir, remote
}

func TestNetworkCommandsRefuseAnUngatedContext(t *testing.T) {
	dir, remote := trackedRepo(t)
	seededRemote(t)
	clone := sddClone(t, model.ProtocolHTTPS)
	ctx := context.Background()
	cases := []struct {
		name string
		run  func() error
	}{
		{name: "fetch", run: func() error { return git.CLI{Dir: dir}.Fetch(ctx) }},
		{name: "push", run: func() error { return git.CLI{Dir: dir}.Push(ctx) }},
		{name: "merge pull", run: func() error { _, err := git.CLI{Dir: dir}.MergePull(ctx); return err }},
		{name: "graph clone", run: func() error {
			return git.CloneGraph(ctx, cloneRepoID, model.CloneSource{First: model.ProtocolHTTPS}, filepath.Join(t.TempDir(), "clone"), model.GraphCloneCone(""))
		}},
		{name: "single-remote clone", run: func() error {
			return git.CloneGraph(ctx, "", model.CloneSource{URL: remote}, filepath.Join(t.TempDir(), "clone"), model.GraphCloneCone(""))
		}},
		{name: "graph clone fetch", run: func() error { return git.CLI{Dir: clone}.Fetch(ctx) }},
		{name: "graph clone push", run: func() error { return git.CLI{Dir: clone}.Push(ctx) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if !errors.Is(err, gitgate.ErrNotAdmitted) {
				t.Errorf("a network command outside the gate should fail with ErrNotAdmitted, got %v", err)
			}
		})
	}
}

func TestAdmittedNetworkCommandIsTraced(t *testing.T) {
	dir, _ := trackedRepo(t)
	target := gitgate.Target{Host: "example.test", Repo: "example.test/acme/work", Protocol: gitgate.HTTPS}
	var traced int64
	err := testGate(t).Run(context.Background(), target, gitgate.Fetch, func(ctx context.Context) error {
		err := git.CLI{Dir: dir}.Fetch(ctx)
		if err != nil {
			return err
		}
		env, _ := gitgate.Env(ctx)
		if len(env) != 1 {
			t.Fatalf("an admission should name one trace file, got %v", env)
		}
		info, err := os.Stat(env[0][len("GIT_TRACE2_EVENT="):])
		if err != nil {
			return err
		}
		traced = info.Size()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if traced == 0 {
		t.Error("git should write its trace into the admission's trace file")
	}
}

func TestUpstreamURL(t *testing.T) {
	dir, remote := trackedRepo(t)
	got, err := git.CLI{Dir: dir}.UpstreamURL(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != remote {
		t.Errorf("the upstream URL should be the tracked remote's, got %q", got)
	}
}
