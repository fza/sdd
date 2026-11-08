package git_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/test"
)

const secondEntryPath = ".sdd/graph/2026/11/07-130000-s-tac-def.md"

// requireLazyFetchBar skips where the installed git cannot bar lazy fetches,
// so sdd leaves them unbarred (20261108-104247-d-tac-k6f).
func requireLazyFetchBar(t *testing.T) {
	t.Helper()
	out, err := exec.Command("git", "version").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !model.GitSupportsLazyFetchBar(string(out)) {
		t.Skipf("%s bars no lazy fetches", strings.TrimSpace(string(out)))
	}
}

// cutOff makes the remote unreachable, so any later fetch fails.
func cutOff(t *testing.T, remote *test.GraphRemote) {
	t.Helper()
	err := os.Rename(remote.Bare, remote.Bare+".gone")
	if err != nil {
		t.Fatal(err)
	}
}

// localCommit makes the clone diverge from upstream with a commit of sdd's.
func localCommit(t *testing.T, dir string) {
	t.Helper()
	path := ".sdd/graph/2026/11/07-140000-s-tac-ghi.md"
	writeFile(t, dir, path, test.MergeEntryFixture(t, "title-a.md"))
	err := git.CLI{Dir: dir}.Commit("sdd: local entry", path)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCloneMergesWithoutTheRemoteAfterAFetch(t *testing.T) {
	remote := seededRemote(t)
	dir := sddClone(t, model.ProtocolHTTPS)
	repo := git.CLI{Dir: dir}
	remote.Commit(t, "upstream entries and code", map[string]string{
		entryPath:       test.MergeEntryFixture(t, "title-b.md"),
		secondEntryPath: test.MergeEntryFixture(t, "title-a.md"),
		"src/code.txt":  "changed code\n",
	})
	localCommit(t, dir)

	err := repo.Fetch(admitted(t, context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	cutOff(t, remote)
	err = repo.Merge(context.Background(), "origin/main", "sdd: merge origin/main")
	if err != nil {
		t.Fatalf("the fetch should bring every content the merge needs: %v", err)
	}
	if !strings.Contains(readFile(t, dir, entryPath), "Beta title") || readFile(t, dir, secondEntryPath) == "" {
		t.Error("the merge should bring the upstream entries")
	}
}

func TestCloneCommandOutsideAnAdmissionFetchesNothing(t *testing.T) {
	requireLazyFetchBar(t)
	remote := seededRemote(t)
	dir := sddClone(t, model.ProtocolHTTPS)
	repo := git.CLI{Dir: dir}
	remote.Commit(t, "upstream entry", map[string]string{entryPath: test.MergeEntryFixture(t, "title-b.md")})
	test.Git(t, dir, "fetch", "-q", "https")
	head := strings.TrimSpace(test.Git(t, dir, "rev-parse", "HEAD"))

	err := repo.Merge(context.Background(), "origin/main", "sdd: merge origin/main")
	if err == nil || !strings.Contains(err.Error(), "lazy fetching disabled") {
		t.Fatalf("a merge lacking contents outside an admission should fail instead of fetching, got %v", err)
	}
	if strings.TrimSpace(test.Git(t, dir, "rev-parse", "HEAD")) != head {
		t.Error("the failed merge should leave HEAD where it was")
	}
	if strings.TrimSpace(test.Git(t, dir, "status", "--porcelain")) != "" {
		t.Error("the failed merge should leave the working tree clean")
	}
}

// Upstream commits an earlier sdd fetched without contents and never merged
// get their contents from the next fetch's backfill.
func TestCloneFetchBackfillsContentsAnEarlierFetchLeftOut(t *testing.T) {
	requireLazyFetchBar(t)
	remote := seededRemote(t)
	dir := sddClone(t, model.ProtocolHTTPS)
	repo := git.CLI{Dir: dir}
	remote.Commit(t, "upstream entry and code", map[string]string{
		entryPath:      test.MergeEntryFixture(t, "title-b.md"),
		"src/code.txt": "changed code\n",
	})
	test.Git(t, dir, "fetch", "-q", "https")
	remote.Commit(t, "another upstream entry", map[string]string{secondEntryPath: test.MergeEntryFixture(t, "title-a.md")})
	localCommit(t, dir)

	err := repo.Fetch(admitted(t, context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	cutOff(t, remote)
	err = repo.Merge(context.Background(), "origin/main", "sdd: merge origin/main")
	if err != nil {
		t.Fatalf("the backfill should bring the contents the earlier fetch left out: %v", err)
	}
	if !strings.Contains(readFile(t, dir, entryPath), "Beta title") {
		t.Error("the merge should bring the entry the earlier fetch left without contents")
	}
}

// lazyFetches runs op inside an admission the gate traces and counts the
// fetches of missing contents git started, each one connection.
func lazyFetches(t *testing.T, op func(ctx context.Context) error) int {
	t.Helper()
	target := gitgate.Target{Host: "example.test", Repo: string(cloneRepoID), Protocol: gitgate.HTTPS}
	var trace string
	err := testGate(t).Run(context.Background(), target, gitgate.Fetch, func(ctx context.Context) error {
		err := op(ctx)
		if err != nil {
			return err
		}
		env, _ := gitgate.Env(ctx)
		content, err := os.ReadFile(strings.TrimPrefix(env[0], "GIT_TRACE2_EVENT="))
		trace = string(content)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	fetches := 0
	for line := range strings.Lines(trace) {
		if strings.Contains(line, `"event":"child_start"`) && strings.Contains(line, `"fetch"`) && strings.Contains(line, `"--stdin"`) {
			fetches++
		}
	}
	return fetches
}

// In a repository holding only its graph, the fetch brings every content the
// merge needs on its one connection.
func TestCloneFetchOfGraphChangesNeedsNoFurtherFetch(t *testing.T) {
	remote := seededRemote(t)
	dir := sddClone(t, model.ProtocolHTTPS)
	remote.Commit(t, "upstream entries", map[string]string{
		entryPath:       test.MergeEntryFixture(t, "title-b.md"),
		secondEntryPath: test.MergeEntryFixture(t, "title-a.md"),
	})

	fetches := lazyFetches(t, git.CLI{Dir: dir}.Fetch)
	if fetches != 0 {
		t.Errorf("the fetch should bring the entries' contents itself, got %d further fetches", fetches)
	}
}

// Code files outside the sparse set arrive as deltas against versions the
// clone never fetched; their bases come in one batch, not one connection
// each.
func TestCloneFetchFetchesMissingDeltaBasesInOneBatch(t *testing.T) {
	remote := seededRemote(t)
	code := map[string]string{}
	for i := range 5 {
		code[fmt.Sprintf("src/module%d.txt", i)] = strings.Repeat(fmt.Sprintf("line of module %d code that deltas compress well\n", i), 200)
	}
	remote.Commit(t, "code", code)
	dir := sddClone(t, model.ProtocolHTTPS)
	for name, content := range code {
		code[name] = content + "one more line\n"
	}
	remote.Commit(t, "code edits", code)

	fetches := lazyFetches(t, git.CLI{Dir: dir}.Fetch)
	if fetches > 1 {
		t.Errorf("missing delta bases should arrive in at most one batch, got %d fetches", fetches)
	}
}

// The checkout that ends a fresh clone fetches the cone's contents inside the
// clone's admission, so the gate's trace sees that fetch.
func TestCloneCheckoutFetchIsTraced(t *testing.T) {
	seededRemote(t)
	dir := filepath.Join(t.TempDir(), "clone")
	fetches := lazyFetches(t, func(ctx context.Context) error {
		return git.CloneGraph(ctx, cloneRepoID, model.CloneSource{First: model.ProtocolHTTPS}, dir, model.GraphCloneCone(""))
	})
	if fetches == 0 {
		t.Error("the checkout's fetch of the cone's contents should reach the gate's trace")
	}
}
