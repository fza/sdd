package cliapp_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/syncstate"
	"github.com/networkteam/sdd/test"
)

const graphCloneRepoID = model.RepoID("example.test/acme/app-sdd")

func TestMain(m *testing.M) {
	test.RunTestsAsSDD(m)
}

// graphRepoCode builds a code repository whose config names a graph
// repository with graph_repo, beside a project_dir leading nowhere, and the
// graph repository on a remote.
func graphRepoCode(t *testing.T) (code string, remote *test.GraphRemote) {
	t.Helper()
	remote = test.NewGraphRemote(t, graphCloneRepoID)
	remote.Commit(t, "seed the graph", map[string]string{
		".sdd/config.yaml":     "graph_dir: .sdd/graph\nrepo_id: " + string(graphCloneRepoID) + "\ndefault_branch: main\nlanguage: en\nverify:\n  mode: off\n",
		".sdd/graph/.keep":     "",
		".gitignore":           ".sdd/tmp/\n.sdd/config.local.yaml\n",
		"src/never-checked.go": "package code\n",
	})
	code = filepath.Join(canonicalTemp(t), "code")
	err := os.MkdirAll(code, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	initRepo(t, code)
	writeFile(t, filepath.Join(code, ".sdd", "config.yaml"), "graph_repo: "+string(graphCloneRepoID)+"\nproject_dir: ../nowhere\ngraph_dir: "+model.GraphElsewhereFile+"\nskill_scope: project\n")
	writeFile(t, filepath.Join(code, model.GraphElsewhereFile), model.GraphElsewhereText(graphCloneRepoID))
	gitIn(t, code, "add", ".sdd")
	gitIn(t, code, "commit", "-q", "-m", "point at the graph repository")
	writeFile(t, filepath.Join(code, "xdg-config", "sdd", "config.yaml"), "participant: Ada\n")
	return code, remote
}

func cloneDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(os.Getenv("XDG_DATA_HOME"), "sdd", "repos", filepath.FromSlash(string(graphCloneRepoID)))
}

// The first command needing the graph clones it, and a capture in the code
// repository lands in the clone, is pushed, and leaves the code repository
// untouched, though project_dir is set as well.
func TestGraphRepoCaptureGoesThroughTheClone(t *testing.T) {
	code, remote := graphRepoCode(t)
	codeHead := headSubject(t, code)

	out, err := runAppErr(t, code, "repo", "path", string(graphCloneRepoID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != cloneDir(t) {
		t.Errorf("repo path should print the clone's place under the data dir, got %q", out)
	}

	captureSignal(t, code)

	clone := cloneDir(t)
	entries := graphEntries(t, filepath.Join(clone, ".sdd", "graph"))
	if len(entries) != 1 {
		t.Fatalf("the capture should land in the clone's graph, got %v", entries)
	}
	_, statErr := os.Stat(filepath.Join(clone, "src"))
	if statErr == nil {
		t.Error("the clone should check out only .sdd")
	}
	_, pushed := remote.Show(t, ".sdd/graph/"+filepath.ToSlash(entries[0]))
	if !pushed {
		t.Error("the capture should be pushed to the graph repository")
	}
	if headSubject(t, code) != codeHead {
		t.Error("the code repository should get no commit")
	}
	if status := gitIn(t, code, "status", "--porcelain", "--untracked-files=all", "--", ".sdd"); strings.TrimSpace(status) != "" {
		t.Errorf("the code repository's .sdd should stay untouched:\n%s", status)
	}
}

// Upstream commits nobody made through sdd, touching code beside the graph,
// still merge into the clone: sync never holds there.
func TestGraphRepoSyncMergesEveryUpstreamCommit(t *testing.T) {
	code, remote := graphRepoCode(t)
	captureSignal(t, code)
	handEntry := ".sdd/graph/2026/11/07-150000-s-tac-hnd.md"
	remote.Commit(t, "a person's own commit", map[string]string{
		handEntry:          "---\ntype: signal\nlayer: tactical\nkind: gap\nconfidence: high\nparticipants: [Bo]\nsummary: Written in a person's own clone.\n---\n\nBy hand.\n",
		"src/elsewhere.go": "package code\n",
	})

	runApp(t, code, "sync")

	_, statErr := os.Stat(filepath.Join(cloneDir(t), filepath.FromSlash(handEntry)))
	if statErr != nil {
		t.Errorf("the upstream entry should arrive in the clone: %v", statErr)
	}
}

// `sdd init --graph-repo` against an empty graph repository sets .sdd/ up in
// sdd's clone and pushes it, then commits only the pointer, the guard and the
// agent surfaces in the code repository. An sdd that knows no graph_repo then
// reads the guard as graph_dir and fails every capture, writing nothing.
func TestInitGraphRepoAndTheStockGuard(t *testing.T) {
	remote := test.NewGraphRemote(t, graphCloneRepoID)
	code := filepath.Join(canonicalTemp(t), "code")
	err := os.MkdirAll(code, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	initRepo(t, code)

	runApp(t, code, "init", "--graph-repo", "https://example.test/acme/app-sdd.git", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude")

	committed, found := remote.Show(t, ".sdd/config.yaml")
	if !found || !strings.Contains(committed, "repo_id: "+string(graphCloneRepoID)) {
		t.Fatalf("the graph repository should be set up and pushed:\n%s", committed)
	}
	pointer := readFile(t, filepath.Join(code, ".sdd", "config.yaml"))
	if !strings.Contains(pointer, "graph_repo: "+string(graphCloneRepoID)) || !strings.Contains(pointer, "graph_dir: "+model.GraphElsewhereFile) {
		t.Errorf("the code repository should point at the graph and carry the guard:\n%s", pointer)
	}
	if status := gitIn(t, code, "status", "--porcelain", "--untracked-files=all"); strings.TrimSpace(status) != "" {
		t.Errorf("init should commit everything it wrote in the code repository:\n%s", status)
	}
	shipped := gitIn(t, code, "show", "--name-only", "--format=", "HEAD")
	for _, path := range []string{".sdd/config.yaml", model.GraphElsewhereFile} {
		if !strings.Contains(shipped, path) {
			t.Errorf("the init commit should hold %s:\n%s", path, shipped)
		}
	}
	if strings.Contains(shipped, ".sdd/graph/") {
		t.Errorf("no graph should grow in the code repository:\n%s", shipped)
	}
	if !strings.Contains(readFile(t, filepath.Join(code, ".sdd", "config.local.yaml")), "participant: Ada") {
		t.Error("the participant should be recorded in the code repository's local layer")
	}

	// An sdd without graph_repo support skips the key it does not know.
	writeFile(t, filepath.Join(code, ".sdd", "config.yaml"), strings.Replace(pointer, "graph_repo: "+string(graphCloneRepoID)+"\n", "", 1))
	gitIn(t, code, "commit", "-q", "-am", "read as stock sdd reads it")
	head := headSubject(t, code)
	out, err := runAppErr(t, code,
		"new", "signal", "tactical", "A capture that must not land in the code repository.",
		"--confidence", "high", "--skip-preflight", "--skip-grounding", "--participants", "Ada", "--summary", "A stray capture.", "--title", "Stray capture")
	if err == nil || !strings.Contains(err.Error()+out, "not a directory") {
		t.Errorf("a capture should fail on the guard file, got %v:\n%s", err, out)
	}
	if headSubject(t, code) != head {
		t.Error("the failed capture should commit nothing")
	}
	if status := gitIn(t, code, "status", "--porcelain", "--untracked-files=all"); strings.TrimSpace(status) != "" {
		t.Errorf("the failed capture should write nothing:\n%s", status)
	}
}

// After an upstream force push, a set-aside entry refining one the rewrite
// kept returns and is pushed on its own; a plain new entry, and one failing
// the mechanical pre-flight, wait in the blocked-sync notice of the agent
// that claims it, until they are discarded.
func TestGraphRepoForcePushReaddsOnlyARefiningEntry(t *testing.T) {
	code, remote := graphRepoCode(t)
	remote.Commit(t, "a decision", map[string]string{test.ForcePushDecision: test.ForcePushFixture(t, "decision.md")})
	runApp(t, code, "repo", "path", string(graphCloneRepoID))
	clone := cloneDir(t)
	repo := git.CLI{Dir: clone}
	for path, fixture := range map[string]string{test.ForcePushRefines: "refines.md", test.ForcePushPlain: "plain.md", test.ForcePushUnchecked: "unchecked.md"} {
		writeFile(t, filepath.Join(clone, filepath.FromSlash(path)), test.ForcePushFixture(t, fixture))
		err := repo.Commit("sdd: capture "+fixture, path)
		if err != nil {
			t.Fatal(err)
		}
	}
	remote.Rewrite(t, "rewritten history", map[string]string{
		".sdd/config.yaml":      "graph_dir: .sdd/graph\nrepo_id: " + string(graphCloneRepoID) + "\ndefault_branch: main\nlanguage: en\nverify:\n  mode: off\n",
		".gitignore":            ".sdd/tmp/\n.sdd/config.local.yaml\n",
		test.ForcePushDecision:  test.ForcePushFixture(t, "decision.md"),
		"src/rewritten-code.go": "package code\n",
	})

	runApp(t, code, "sync")

	_, pushed := remote.Show(t, test.ForcePushRefines)
	if !pushed {
		t.Error("the refining entry should be re-added and pushed")
	}
	for _, waiting := range []string{test.ForcePushPlain, test.ForcePushUnchecked} {
		_, pushed = remote.Show(t, waiting)
		if pushed {
			t.Errorf("%s should wait for an agent, not be pushed", waiting)
		}
	}
	graphDir := filepath.Join(clone, ".sdd", "graph")
	blocked, err := syncstate.ReadBlocked(t.Context(), graphDir)
	if err != nil {
		t.Fatal(err)
	}
	if blocked == nil || blocked.Kind != model.SyncBlockedSetAside || !slices.Equal(blocked.Paths, []string{test.ForcePushPlain, test.ForcePushUnchecked}) {
		t.Fatalf("the plain entry and the one failing pre-flight should be recorded as set aside, got %+v", blocked)
	}
	opts, err := syncstate.HandlerOptions(t.Context(), graphDir, handlers.Options{GraphDir: graphDir})
	if err != nil {
		t.Fatal(err)
	}
	var notice string
	err = handlers.New(opts).ClaimSyncNotice(t.Context(), &command.ClaimSyncNoticeCmd{Session: "s_1", OnNotice: func(text string) { notice = text }})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notice, "You own this block") || !strings.Contains(notice, test.ForcePushPlain) || !strings.Contains(notice, blocked.Detail) {
		t.Errorf("the claiming agent's notice should name the set-aside entry and where it lies:\n%s", notice)
	}

	for _, discarded := range blocked.Paths {
		err = os.Remove(filepath.Join(blocked.Detail, filepath.FromSlash(discarded)))
		if err != nil {
			t.Fatal(err)
		}
	}
	runApp(t, code, "sync")
	blocked, err = syncstate.ReadBlocked(t.Context(), graphDir)
	if err != nil {
		t.Fatal(err)
	}
	if blocked != nil {
		t.Errorf("sync should clear the block once no set-aside entry is left, got %+v", blocked)
	}
}

// `sdd config set --local` in a code repository pointing at a graph
// repository writes the code repository's config.local.yaml, the layer every
// command there reads; sdd's clone gets no local file.
func TestGraphRepoConfigSetLocalWritesTheCodeRepository(t *testing.T) {
	code, _ := graphRepoCode(t)

	runApp(t, code, "config", "set", "--local", "connected.write", "true")

	local := readFile(t, filepath.Join(code, ".sdd", "config.local.yaml"))
	if !strings.Contains(local, "write: true") {
		t.Errorf("the setting should land in the code repository's local file:\n%s", local)
	}
	_, statErr := os.Stat(filepath.Join(cloneDir(t), ".sdd", "config.local.yaml"))
	if statErr == nil {
		t.Error("sdd's clone should get no local file")
	}
	out, err := runAppErr(t, code, "config", "get", "connected.write")
	if err != nil || !strings.Contains(out, "true") {
		t.Errorf("reading the setting back should see the code repository's layer, got %v:\n%s", err, out)
	}
}
