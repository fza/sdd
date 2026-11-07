package git_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/test"
)

const (
	cloneRepoID = model.RepoID("example.test/acme/app-sdd")
	entryPath   = ".sdd/graph/2026/11/07-120000-s-tac-abc.md"
)

func TestMain(m *testing.M) {
	test.RunTestsAsSDD(m)
}

// seededRemote is a graph repository holding a config, one entry and code
// outside the graph.
func seededRemote(t *testing.T) *test.GraphRemote {
	t.Helper()
	remote := test.NewGraphRemote(t, cloneRepoID)
	remote.Commit(t, "seed", map[string]string{
		".sdd/config.yaml": "graph_dir: .sdd/graph\nllm:\n  model: base\n",
		entryPath:          test.MergeEntryFixture(t, "base.md"),
		"src/code.txt":     "code\n",
	})
	return remote
}

func sddClone(t *testing.T, first string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "clone")
	err := git.CloneGraph(admitted(t, context.Background()), cloneRepoID, first, dir, model.GraphCloneCone(""))
	if err != nil {
		t.Fatalf("CloneGraph: %v", err)
	}
	err = git.CLI{Dir: dir}.KeepGraphClone(context.Background(), []string{".sdd/graph/" + model.EntryPathGlob + " merge=" + model.EntryMergeDriver, model.ConfigMergeAttributeLine})
	if err != nil {
		t.Fatalf("KeepGraphClone: %v", err)
	}
	return dir
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(path, []byte(content), 0o644)
	if err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func capturedLog(t *testing.T) (context.Context, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	return admitted(t, slogutils.WithLogger(context.Background(), slog.New(slog.NewTextHandler(&buf, nil)))), &buf
}

func TestCloneGraphIsBloblessAndSparse(t *testing.T) {
	seededRemote(t)
	dir := sddClone(t, model.ProtocolSSH)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if strings.Join(names, " ") != ".git .sdd "+model.GraphCloneMarker {
		t.Errorf("the worktree should hold only .git, .sdd and the marker, got %v", names)
	}
	if !strings.Contains(readFile(t, dir, entryPath), "Base title") {
		t.Error("the graph's entries should be checked out")
	}
	filter := strings.TrimSpace(test.Git(t, dir, "config", "remote.https.partialclonefilter"))
	if filter != "blob:none" {
		t.Errorf("the clone should be blobless, got filter %q", filter)
	}
	if !git.IsGraphClone(dir) {
		t.Error("the clone should be recognised as sdd's")
	}
	if strings.TrimSpace(test.Git(t, dir, "config", "sdd.remote")) != model.ProtocolHTTPS {
		t.Error("with ssh unreachable the clone should record https")
	}
}

func TestKeepGraphCloneDiscardsWhatSddDidNotMake(t *testing.T) {
	remote := seededRemote(t)
	dir := sddClone(t, model.ProtocolHTTPS)
	ctx := admitted(t, context.Background())
	repo := git.CLI{Dir: dir}

	sddEntry := ".sdd/graph/2026/11/07-130000-s-tac-sdd.md"
	writeFile(t, dir, sddEntry, "---\ntype: signal\n---\nby sdd\n")
	err := repo.Commit("sdd: capture by sdd", sddEntry)
	if err != nil {
		t.Fatal(err)
	}
	handEntry := ".sdd/graph/2026/11/07-140000-s-tac-hnd.md"
	writeFile(t, dir, handEntry, "---\ntype: signal\n---\nby hand\n")
	test.Git(t, dir, "add", handEntry)
	test.Git(t, dir, "commit", "-q", "-m", "sdd: looks like sdd but is a hand commit")
	writeFile(t, dir, entryPath, "edited by hand\n")
	writeFile(t, dir, ".sdd/graph/untracked.md", "untracked\n")
	err = os.Remove(filepath.Join(dir, model.GraphCloneMarker))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, ".git/info/exclude", "# emptied by hand\n")

	err = repo.KeepGraphClone(ctx, nil)
	if err != nil {
		t.Fatalf("KeepGraphClone: %v", err)
	}

	if strings.Contains(readFile(t, dir, entryPath), "edited by hand") {
		t.Error("an uncommitted edit should be restored")
	}
	for _, gone := range []string{".sdd/graph/untracked.md", handEntry} {
		_, statErr := os.Stat(filepath.Join(dir, filepath.FromSlash(gone)))
		if statErr == nil {
			t.Errorf("%s should be gone", gone)
		}
	}
	if readFile(t, dir, sddEntry) != "---\ntype: signal\n---\nby sdd\n" {
		t.Error("sdd's own commit should stay")
	}
	if readFile(t, dir, model.GraphCloneMarker) != model.GraphCloneMarkerText {
		t.Error("the marker should be restored")
	}
	if !strings.Contains(readFile(t, dir, ".git/info/exclude"), model.GraphCloneExcludeLine+"\n") {
		t.Error("the marker's exclude line should be restored")
	}
	if strings.TrimSpace(test.Git(t, dir, "status", "--porcelain")) != "" {
		t.Error("the clone should be clean, the marker ignored")
	}

	err = repo.Push(ctx)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	_, pushed := remote.Show(t, sddEntry)
	if !pushed {
		t.Error("sdd's commit should reach the remote")
	}
	_, leaked := remote.Show(t, handEntry)
	if leaked {
		t.Error("the hand commit should never reach the remote")
	}
}

// A hand commit made right before a push is dropped by the push itself.
func TestKeepGraphCloneRunsBeforeAnyHandCommitIsPushed(t *testing.T) {
	remote := seededRemote(t)
	dir := sddClone(t, model.ProtocolHTTPS)
	handEntry := ".sdd/graph/2026/11/07-140000-s-tac-hnd.md"
	writeFile(t, dir, handEntry, "by hand\n")
	test.Git(t, dir, "add", handEntry)
	test.Git(t, dir, "commit", "-q", "-m", "hand")

	err := git.CLI{Dir: dir}.KeepGraphClone(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ahead := strings.TrimSpace(test.Git(t, dir, "rev-list", "--count", "@{u}..HEAD"))
	if ahead != "0" {
		t.Errorf("no unrecorded commit should stay ahead of upstream, got %s", ahead)
	}
	if remote.Head(t) != strings.TrimSpace(test.Git(t, dir, "rev-parse", "HEAD")) {
		t.Error("the clone should sit on the upstream tip")
	}
}

func TestCloneMergesTitleChangesThroughTheEntryDriver(t *testing.T) {
	remote := seededRemote(t)
	remote.Commit(t, "a committed driver for every markdown file", map[string]string{".gitattributes": "*.md merge=binary\n"})
	ctx, _ := capturedLog(t)
	first := sddClone(t, model.ProtocolHTTPS)
	second := sddClone(t, model.ProtocolHTTPS)

	writeFile(t, first, entryPath, test.MergeEntryFixture(t, "title-b.md"))
	err := git.CLI{Dir: first}.Commit("sdd: retitle", entryPath)
	if err != nil {
		t.Fatal(err)
	}
	err = git.CLI{Dir: first}.Push(ctx)
	if err != nil {
		t.Fatal(err)
	}

	repo := git.CLI{Dir: second}
	writeFile(t, second, entryPath, test.MergeEntryFixture(t, "title-a.md"))
	err = repo.Commit("sdd: retitle elsewhere", entryPath)
	if err != nil {
		t.Fatal(err)
	}
	err = repo.Fetch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	predicted, err := repo.MergeTreePredict(ctx, "HEAD", "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	if len(predicted) > 0 {
		t.Errorf("the driver should leave nothing to conflict, got %v", predicted)
	}
	err = repo.Merge(ctx, "origin/main", "sdd: merge origin/main")
	if err != nil {
		t.Fatalf("the merge should succeed: %v", err)
	}
	merged := readFile(t, second, entryPath)
	if !strings.Contains(merged, "title: Alpha title\n") || strings.Contains(merged, "<<<<<<<") {
		t.Errorf("the title sorting first should win without conflict:\n%s", merged)
	}
	drivers, _ := exec.Command("git", "-C", second, "config", "--local", "--get-regexp", "^merge\\.").Output()
	if len(drivers) > 0 {
		t.Error("the clone's config should hold no merge driver")
	}
	ahead := strings.TrimSpace(test.Git(t, second, "rev-list", "--count", "@{u}..HEAD"))
	err = repo.KeepGraphClone(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(test.Git(t, second, "rev-list", "--count", "@{u}..HEAD")) != ahead {
		t.Error("sdd's own merge commit should be recorded and survive upkeep")
	}
}

func TestCloneLetsAnUpstreamDeletionWin(t *testing.T) {
	remote := seededRemote(t)
	ctx, log := capturedLog(t)
	dir := sddClone(t, model.ProtocolHTTPS)
	repo := git.CLI{Dir: dir}

	own := remote.Clone(t)
	test.Git(t, own, "rm", "-q", entryPath)
	test.Git(t, own, "commit", "-q", "-m", "drop the entry")
	test.Git(t, own, "push", "-q", "origin", "HEAD:refs/heads/main")
	deleting := strings.TrimSpace(test.Git(t, own, "rev-parse", "HEAD"))

	writeFile(t, dir, entryPath, test.MergeEntryFixture(t, "title-a.md"))
	err := repo.Commit("sdd: retitle", entryPath)
	if err != nil {
		t.Fatal(err)
	}
	err = repo.Fetch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	predicted, err := repo.MergeTreePredict(ctx, "HEAD", "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	if len(predicted) > 0 {
		t.Errorf("an upstream deletion should not count as a conflict, got %v", predicted)
	}
	err = repo.Merge(ctx, "origin/main", "sdd: merge origin/main")
	if err != nil {
		t.Fatalf("the merge should resolve with the deletion: %v", err)
	}
	_, statErr := os.Stat(filepath.Join(dir, filepath.FromSlash(entryPath)))
	if statErr == nil {
		t.Error("the deletion should win")
	}
	if !strings.Contains(log.String(), entryPath) || !strings.Contains(log.String(), deleting) {
		t.Errorf("a notice should name the entry and the deleting commit:\n%s", log)
	}
}

func TestCloneMergesConfigPerKey(t *testing.T) {
	remote := seededRemote(t)
	ctx, log := capturedLog(t)
	dir := sddClone(t, model.ProtocolHTTPS)
	repo := git.CLI{Dir: dir}
	remote.Commit(t, "upstream model", map[string]string{".sdd/config.yaml": "graph_dir: .sdd/graph\nllm:\n  model: upstream\n"})

	writeFile(t, dir, ".sdd/config.yaml", "graph_dir: .sdd/graph\nllm:\n  model: local\ndependencies:\n  - example.test/acme/other\n")
	err := repo.Commit("sdd: connect", ".sdd/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	err = repo.Fetch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = repo.Merge(ctx, "origin/main", "sdd: merge origin/main")
	if err != nil {
		t.Fatalf("the config clash should merge: %v", err)
	}
	merged := readFile(t, dir, ".sdd/config.yaml")
	if !strings.Contains(merged, "model: upstream") || !strings.Contains(merged, "example.test/acme/other") {
		t.Errorf("upstream should win the clash and the local addition stay:\n%s", merged)
	}
	if !strings.Contains(log.String(), "llm.model") {
		t.Errorf("a notice should name the key both sides changed:\n%s", log)
	}
}

func TestCloneProtocolChoice(t *testing.T) {
	remote := seededRemote(t)
	dir := sddClone(t, model.ProtocolSSH)
	repo := git.CLI{Dir: dir}
	ctx := admitted(t, context.Background())
	recorded := func() string { return strings.TrimSpace(test.Git(t, dir, "config", "sdd.remote")) }
	since := func() string { return strings.TrimSpace(test.Git(t, dir, "config", "sdd.remoteSince")) }
	if recorded() != model.ProtocolHTTPS {
		t.Fatal("with ssh unreachable the clone should fall back to https")
	}

	remote.AllowSSH(t)
	recent := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	test.Git(t, dir, "config", "sdd.remoteSince", recent)
	err := repo.Fetch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if recorded() != model.ProtocolHTTPS || since() != recent {
		t.Error("a recorded https should be used first while it works")
	}

	test.Git(t, dir, "config", "sdd.remoteSince", time.Now().Add(-25*time.Hour).UTC().Format(time.RFC3339))
	err = repo.Fetch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if recorded() != model.ProtocolSSH {
		t.Error("after a day on https a working ssh should be tried first and recorded")
	}
	if strings.TrimSpace(test.Git(t, dir, "config", "branch.main.remote")) != model.ProtocolSSH {
		t.Error("the branch should fetch and push over the recorded protocol")
	}
}

func TestCloneRetriesBrokenSSHOncePerDay(t *testing.T) {
	seededRemote(t)
	dir := sddClone(t, model.ProtocolHTTPS)
	stale := time.Now().Add(-25 * time.Hour).UTC().Format(time.RFC3339)
	test.Git(t, dir, "config", "sdd.remoteSince", stale)

	err := git.CLI{Dir: dir}.Fetch(admitted(t, context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(test.Git(t, dir, "config", "sdd.remote")) != model.ProtocolHTTPS {
		t.Error("a failing ssh retry should leave the clone on https")
	}
	renewed, err := time.Parse(time.RFC3339, strings.TrimSpace(test.Git(t, dir, "config", "sdd.remoteSince")))
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(renewed) > time.Hour {
		t.Error("a failing ssh retry should renew the https record so the next retry waits a day")
	}
}

// With no commit to drop, the upkeep's own restore undoes a hand edit, staged
// or not.
func TestKeepGraphCloneRestoresAHandEditOnItsOwn(t *testing.T) {
	seededRemote(t)
	dir := sddClone(t, model.ProtocolHTTPS)
	writeFile(t, dir, entryPath, "edited by hand\n")
	writeFile(t, dir, ".sdd/config.yaml", "staged by hand\n")
	test.Git(t, dir, "add", ".sdd/config.yaml")

	err := git.CLI{Dir: dir}.KeepGraphClone(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readFile(t, dir, entryPath), "edited by hand") || strings.Contains(readFile(t, dir, ".sdd/config.yaml"), "staged by hand") {
		t.Error("hand edits should be restored")
	}
}

// A local config planted in the clone is ignored there, yet the upkeep removes
// it: a graph_repo project's local layer is the code repository's.
func TestKeepGraphCloneRemovesAPlantedLocalConfig(t *testing.T) {
	seededRemote(t)
	dir := sddClone(t, model.ProtocolHTTPS)
	writeFile(t, dir, ".sdd/config.local.yaml", "connected:\n  write: true\n")

	err := git.CLI{Dir: dir}.KeepGraphClone(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	_, statErr := os.Stat(filepath.Join(dir, ".sdd", "config.local.yaml"))
	if statErr == nil {
		t.Error("a local config in the clone should be removed")
	}
}
