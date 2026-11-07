package git_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/test"
)

const (
	pushedEntry = ".sdd/graph/2026/11/07-123000-s-tac-psh.md"
	goneEntry   = ".sdd/graph/2026/11/07-124000-s-tac-gon.md"
	heldEntry   = ".sdd/graph/2026/11/07-125000-s-tac-hld.md"
	handEntry   = ".sdd/graph/2026/11/07-126000-s-tac-hnd.md"
	attachment  = ".sdd/graph/2026/11/07-121000-s-tac-pln/rows.csv"
)

// forcePushedClone is a clone holding sdd's work the rewritten upstream no
// longer has: a pushed entry, a retitled surviving entry, a retitled entry
// the rewrite drops, an unpushed entry with an attachment, an unpushed entry
// the rewrite holds too, and a hand commit no upkeep dropped yet. It returns
// the clone, the tip before the rewrite and the rewritten tip.
func forcePushedClone(t *testing.T) (dir, previous, rewritten string) {
	t.Helper()
	remote := seededRemote(t)
	remote.Commit(t, "an entry the rewrite drops", map[string]string{goneEntry: test.MergeEntryFixture(t, "base.md")})
	dir = sddClone(t, model.ProtocolHTTPS)
	repo := git.CLI{Dir: dir}
	ctx := admitted(t, context.Background())
	err := repo.Fetch(ctx)
	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, dir, pushedEntry, test.ForcePushFixture(t, "plain.md"))
	err = repo.Commit("sdd: capture a pushed entry", pushedEntry)
	if err != nil {
		t.Fatal(err)
	}
	err = repo.Push(ctx)
	if err != nil {
		t.Fatal(err)
	}
	previous = remote.Head(t)

	writeFile(t, dir, entryPath, test.MergeEntryFixture(t, "title-b.md"))
	writeFile(t, dir, goneEntry, test.MergeEntryFixture(t, "title-b.md"))
	err = repo.Commit("sdd: retitle", entryPath, goneEntry)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, test.ForcePushPlain, test.ForcePushFixture(t, "plain.md"))
	writeFile(t, dir, attachment, "rows\n")
	err = repo.Commit("sdd: capture a gap", test.ForcePushPlain, attachment)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, heldEntry, test.ForcePushFixture(t, "plain.md"))
	err = repo.Commit("sdd: capture what the rewrite holds", heldEntry)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, handEntry, test.ForcePushFixture(t, "plain.md"))
	test.Git(t, dir, "add", handEntry)
	test.Git(t, dir, "commit", "-q", "-m", "a hand commit")

	rewritten = remote.Rewrite(t, "rewritten history", map[string]string{
		".sdd/config.yaml": "graph_dir: .sdd/graph\nllm:\n  model: base\n",
		entryPath:          strings.Replace(test.MergeEntryFixture(t, "base.md"), "Second line.", "Second line, rewritten.", 1),
		heldEntry:          test.ForcePushFixture(t, "plain.md"),
	})
	return dir, previous, rewritten
}

// A fetch meeting a force push moves the clone onto the rewritten tip, keeps
// sdd's title change where its entry survives, and sets aside only the entry
// and attachment sdd had not pushed.
func TestCloneFollowsAForcePush(t *testing.T) {
	dir, previous, rewritten := forcePushedClone(t)
	repo := git.CLI{Dir: dir}
	ctx, log := capturedLog(t)

	err := repo.Fetch(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if parent := strings.TrimSpace(test.Git(t, dir, "rev-parse", "HEAD^")); parent != rewritten {
		t.Error("the clone should move onto the rewritten tip, never merge the old history")
	}
	surviving := readFile(t, dir, entryPath)
	if !strings.Contains(surviving, "title: Beta title\n") || !strings.Contains(surviving, "rewritten.") {
		t.Errorf("sdd's title change should be re-derived onto the rewritten entry:\n%s", surviving)
	}
	for _, dropped := range []string{goneEntry, pushedEntry, test.ForcePushPlain, handEntry} {
		_, statErr := os.Stat(filepath.Join(dir, filepath.FromSlash(dropped)))
		if statErr == nil {
			t.Errorf("%s should not stay in the graph", dropped)
		}
	}
	setAside, err := repo.SetAside(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(setAside.Entries, []string{test.ForcePushPlain}) {
		t.Errorf("only the unpushed entry should be set aside, got %v", setAside.Entries)
	}
	if setAside.Old != previous || setAside.New != rewritten {
		t.Error("the set-aside record should name the tips of the rewrite")
	}
	_, statErr := os.Stat(filepath.Join(setAside.Dir, filepath.FromSlash(attachment)))
	if statErr != nil {
		t.Errorf("the unpushed attachment should be set aside: %v", statErr)
	}
	if !strings.Contains(log.String(), previous[:12]) || !strings.Contains(log.String(), rewritten[:12]) {
		t.Errorf("the notice should name the old and new tip:\n%s", log.String())
	}

	err = repo.ReaddSetAside(ctx, test.ForcePushPlain)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(test.Git(t, dir, "status", "--porcelain")) != "" {
		t.Error("the re-added entry should be committed")
	}
	readded := strings.TrimSpace(test.Git(t, dir, "ls-tree", "-r", "--name-only", "HEAD", "--", ".sdd/graph"))
	if !strings.Contains(readded, test.ForcePushPlain) || !strings.Contains(readded, attachment) {
		t.Errorf("the entry should return with its attachment:\n%s", readded)
	}
	err = repo.KeepGraphClone(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(test.Git(t, dir, "rev-list", "--count", "@{u}..HEAD")) != "2" {
		t.Error("the re-derive and re-add commits should be recorded as sdd's and survive upkeep")
	}
	setAside, err = repo.SetAside(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(setAside.Entries) > 0 {
		t.Errorf("a re-added entry should leave the set-aside files, got %v", setAside.Entries)
	}
}

// A round interrupted after fetching the rewrite still follows it: the
// upkeep that opens the next round sets aside instead of replaying.
func TestKeepGraphCloneFollowsAForcePushFetchedBefore(t *testing.T) {
	dir, _, rewritten := forcePushedClone(t)
	test.Git(t, dir, "fetch", "-q", "https")

	err := git.CLI{Dir: dir}.KeepGraphClone(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if parent := strings.TrimSpace(test.Git(t, dir, "rev-parse", "HEAD^")); parent != rewritten {
		t.Error("the upkeep should move the clone onto the rewritten tip")
	}
	setAside, err := git.CLI{Dir: dir}.SetAside(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(setAside.Entries, []string{test.ForcePushPlain}) {
		t.Errorf("the unpushed entry should be set aside rather than replayed, got %v", setAside.Entries)
	}
}
