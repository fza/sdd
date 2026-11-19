package cliapp_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/test"
)

const (
	movedFirst  = "20261101-100000-s-tac-one"
	movedSecond = "20261102-100000-d-tac-two"
	movedMeta   = "{\n  \"graph_schema_version\": 1\n}\n"
)

func movedEntry(summary string) string {
	return "---\ntype: signal\nkind: gap\nlayer: tactical\nconfidence: high\nparticipants: [Ada]\nsummary: " + summary + "\n---\n\n" + summary + "\n"
}

func entryPath(graphDir, id string) string {
	rel, err := model.IDToRelPath(id)
	if err != nil {
		panic(err)
	}
	return filepath.Join(graphDir, rel)
}

// graphRepoToMove is a repository holding its graph in .sdd/graph as a fresh
// init leaves it, the two entries committed one after the other.
func graphRepoToMove(t *testing.T) string {
	t.Helper()
	test.IsolatedHome(t)
	repo := filepath.Join(canonicalTemp(t), "work")
	err := os.MkdirAll(repo, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	writeFile(t, filepath.Join(repo, ".sdd", "config.yaml"), model.FormatConfig(model.PerRepoConfig{GraphDir: model.DefaultGraphDir, DefaultBranch: "main", SkillScope: model.ScopeProject, SupportedAgents: []model.AgentTarget{"claude"}}))
	writeFile(t, filepath.Join(repo, ".sdd", "meta.json"), movedMeta)
	writeFile(t, filepath.Join(repo, ".sdd", ".gitignore"), strings.Join(model.SDDIgnoreLines, "\n")+"\n")
	writeFile(t, filepath.Join(repo, ".gitignore"), "xdg-*/\n")
	writeFile(t, filepath.Join(repo, ".sdd", "graph", ".gitattributes"), model.GraphAttributesLine+"\n")
	writeFile(t, entryPath(filepath.Join(repo, ".sdd", "graph"), movedFirst), movedEntry("The first entry."))
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "add the first entry")
	writeFile(t, entryPath(filepath.Join(repo, ".sdd", "graph"), movedSecond), movedEntry("The second entry."))
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "add the second entry")
	writeFile(t, filepath.Join(repo, ".sdd", "config.local.yaml"), "participant: Ada\n")
	return repo
}

func headRecord(t *testing.T, repo string) model.MoveRecord {
	t.Helper()
	trailers := gitIn(t, repo, "log", "-1", "--format=%(trailers:only,unfold)")
	record, found, err := model.ParseMoveRecord(trailers)
	if err != nil || !found {
		t.Fatalf("the last commit in %s should carry a move record: found=%v err=%v\n%s", repo, found, err, trailers)
	}
	return record
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertMissing(t *testing.T, path, why string) {
	t.Helper()
	_, err := os.Stat(path)
	if err == nil {
		t.Errorf("%s: %s should be gone", why, path)
	}
}

// Inside one repository the graph moves as a rename: history follows it,
// the config, attributes and ignore lines name the new folder, the move
// commit carries the record, and meta.json stays as it was.
func TestMoveTheGraphInsideTheRepository(t *testing.T) {
	repo := graphRepoToMove(t)
	runApp(t, repo, "move", "--graph-dir", "graph")

	for _, id := range []string{movedFirst, movedSecond} {
		if _, err := os.Stat(entryPath(filepath.Join(repo, "graph"), id)); err != nil {
			t.Errorf("entry %s should be in the new folder: %v", id, err)
		}
		assertMissing(t, entryPath(filepath.Join(repo, ".sdd", "graph"), id), "the old folder should hold no entry")
	}
	if config := readText(t, filepath.Join(repo, ".sdd", "config.yaml")); !strings.Contains(config, "graph_dir: graph\n") || !strings.Contains(config, "# SDD configuration") {
		t.Errorf("the config should name the new folder and keep its comments:\n%s", config)
	}
	if attributes := readText(t, filepath.Join(repo, "graph", ".gitattributes")); attributes != model.GraphAttributesLine+"\n" {
		t.Errorf("the merge routing should travel with the graph:\n%s", attributes)
	}
	if ignore := readText(t, filepath.Join(repo, ".gitignore")); ignore != "xdg-*/\n" {
		t.Errorf("the move should leave the root .gitignore alone:\n%s", ignore)
	}
	if meta := readText(t, filepath.Join(repo, ".sdd", "meta.json")); meta != movedMeta {
		t.Errorf("the move should leave meta.json as it was:\n%s", meta)
	}
	record := headRecord(t, repo)
	if record.From.String() != "git-dir:.sdd/graph" || record.To.String() != "git-dir:graph" || record.Entries != 2 {
		t.Errorf("the move commit should record where the graph went and how many entries it took, got %+v", record)
	}
	if subject := headSubject(t, repo); !strings.HasPrefix(subject, "sdd: move graph") {
		t.Errorf("the move should commit in sdd's own format, got %q", subject)
	}
	followed := gitIn(t, repo, "log", "--follow", "--format=%s", "--", filepath.ToSlash(entryPath("graph", movedFirst)))
	if !strings.Contains(followed, "add the first entry") {
		t.Errorf("the history should follow the rename:\n%s", followed)
	}
	if status := strings.TrimSpace(gitIn(t, repo, "status", "--porcelain")); status != "" {
		t.Errorf("the move should leave nothing uncommitted:\n%s", status)
	}
	runApp(t, repo, "show", movedFirst)
	if status := strings.TrimSpace(gitIn(t, repo, "status", "--porcelain")); status != "" {
		t.Errorf("the graph's runtime folders, the former one included, should ignore themselves:\n%s", status)
	}
}

// A graph from a build that wrote its merge routing into the root
// .gitattributes gets the graph directory's own one where it lands.
func TestMoveRoutesTheMergesOfAGraphFromAnEarlierBuild(t *testing.T) {
	repo := graphRepoToMove(t)
	gitIn(t, repo, "rm", "-q", ".sdd/graph/.gitattributes")
	gitIn(t, repo, "commit", "-q", "-m", "a graph from an earlier build")
	runApp(t, repo, "move", "--graph-dir", "graph")

	if attributes := readText(t, filepath.Join(repo, "graph", ".gitattributes")); attributes != model.GraphAttributesLine+"\n" {
		t.Errorf("the graph should get its own merge routing:\n%s", attributes)
	}
	if got := strings.TrimSpace(gitIn(t, repo, "check-attr", "merge", "--", filepath.ToSlash(entryPath("graph", movedFirst)))); !strings.HasSuffix(got, ": merge: "+model.EntryMergeDriver) {
		t.Errorf("a moved entry should route to the entry merge driver, got %q", got)
	}
}

// An entry written into the former folder after the move refuses every
// command, and running the same move again carries it over with its history.
func TestMoveCarriesAStrayEntryOver(t *testing.T) {
	repo := graphRepoToMove(t)
	runApp(t, repo, "move", "--graph-dir", "graph")

	const stray = "20261105-100000-s-tac-new"
	writeFile(t, entryPath(filepath.Join(repo, ".sdd", "graph"), stray), movedEntry("Written by an older build."))
	gitIn(t, repo, "add", ".sdd/graph")
	gitIn(t, repo, "commit", "-q", "-m", "an older build captures in the former folder")
	err := runAppResult(t, repo, "show", movedFirst)
	if err == nil || !strings.Contains(err.Error(), "sdd move --graph-dir graph") {
		t.Fatalf("a stray should refuse the command and name the move that carries it, got %v", err)
	}

	runApp(t, repo, "move", "--graph-dir", "graph")
	if _, err := os.Stat(entryPath(filepath.Join(repo, "graph"), stray)); err != nil {
		t.Errorf("the stray should be carried into the graph: %v", err)
	}
	if record := headRecord(t, repo); record.Entries != 1 || record.To.String() != "git-dir:graph" {
		t.Errorf("the carry should record what it carried, got %+v", record)
	}
	followed := gitIn(t, repo, "log", "--follow", "--format=%s", "--", filepath.ToSlash(entryPath("graph", stray)))
	if !strings.Contains(followed, "an older build captures") {
		t.Errorf("the stray should keep its history:\n%s", followed)
	}
	runApp(t, repo, "show", stray)

	err = runAppResult(t, repo, "move", "--graph-dir", "graph")
	if err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("a move to where the graph lives with nothing to carry should be refused, got %v", err)
	}
}

func TestMoveRefuses(t *testing.T) {
	t.Run("more than one target", func(t *testing.T) {
		repo := graphRepoToMove(t)
		err := runAppResult(t, repo, "move", "--graph-dir", "graph", "--graph-repo", "https://example.test/acme/app-sdd.git")
		if err == nil || !strings.Contains(err.Error(), "exactly one") {
			t.Errorf("two targets should be refused, got %v", err)
		}
	})
	t.Run("a checkout on disk as target", func(t *testing.T) {
		repo := graphRepoToMove(t)
		err := runAppResult(t, repo, "move", "--project-dir", "../elsewhere")
		if err == nil || !strings.Contains(err.Error(), "project-dir") {
			t.Errorf("sdd move should take no --project-dir, got %v", err)
		}
	})
	t.Run("uncommitted graph", func(t *testing.T) {
		repo := graphRepoToMove(t)
		writeFile(t, entryPath(filepath.Join(repo, ".sdd", "graph"), "20261106-100000-s-tac-wip"), movedEntry("Not committed."))
		err := runAppResult(t, repo, "move", "--graph-dir", "graph")
		if err == nil || !strings.Contains(err.Error(), "uncommitted") {
			t.Errorf("a graph folder with uncommitted changes should be refused, got %v", err)
		}
	})
	t.Run("target holding a project", func(t *testing.T) {
		repo := graphRepoToMove(t)
		remote := test.NewGraphRemote(t, graphCloneRepoID)
		remote.Commit(t, "a project of its own", map[string]string{".sdd/config.yaml": "graph_dir: graph\n"})
		err := runAppResult(t, repo, "move", "--graph-repo", "https://example.test/acme/app-sdd.git")
		if err == nil || !strings.Contains(err.Error(), "holds an sdd project already") {
			t.Errorf("a target holding a graph should be refused, got %v", err)
		}
	})
	t.Run("target folder holding entries", func(t *testing.T) {
		repo := graphRepoToMove(t)
		writeFile(t, entryPath(filepath.Join(repo, "graph"), movedFirst), movedEntry("Already here."))
		err := runAppResult(t, repo, "move", "--graph-dir", "graph")
		if err == nil || !strings.Contains(err.Error(), "holds graph entries already") {
			t.Errorf("a target folder holding entries should be refused, got %v", err)
		}
	})
}

// An entry an older build committed into the former folder after the graph
// moved out is carried into the graph repository with the commit that wrote
// it.
func TestMoveCarriesAStrayAcrossRepositories(t *testing.T) {
	repo := graphRepoToMove(t)
	remote := test.NewGraphRemote(t, graphCloneRepoID)
	runApp(t, repo, "move", "--graph-repo", "https://example.test/acme/app-sdd.git")
	clone := cloneDir(t)

	const stray = "20261105-100000-s-tac-old"
	writeFile(t, entryPath(filepath.Join(repo, ".sdd", "graph"), stray), movedEntry("Written by an older build."))
	gitIn(t, repo, "add", "--force", ".sdd/graph")
	gitIn(t, repo, "commit", "-q", "-m", "an older build captures in the code repository")
	err := runAppResult(t, repo, "show", movedFirst)
	if err == nil || !strings.Contains(err.Error(), "sdd move --graph-repo "+string(graphCloneRepoID)) {
		t.Fatalf("a stray should refuse the command and name the move that carries it, got %v", err)
	}

	runApp(t, repo, "move", "--graph-repo", "https://example.test/acme/app-sdd.git")
	if _, found := remote.Show(t, filepath.ToSlash(entryPath("graph", stray))); !found {
		t.Error("the stray should be carried into the graph repository")
	}
	assertMissing(t, entryPath(filepath.Join(repo, ".sdd", "graph"), stray), "the stray leaves the former folder")
	if log := gitIn(t, clone, "log", "--format=%s"); !strings.Contains(log, "an older build captures") {
		t.Errorf("the graph repository should replay the commit that wrote the stray:\n%s", log)
	}
	if headRecord(t, repo).ID != headRecord(t, clone).ID {
		t.Error("both repositories should record the carry")
	}
	runApp(t, repo, "show", stray)
}

// A move out to a repository sdd clones itself publishes the graph there,
// and the code repository keeps only the graph_repo pointer.
func TestMoveTheGraphOutToAGraphRepoAndBack(t *testing.T) {
	repo := graphRepoToMove(t)
	remote := test.NewGraphRemote(t, graphCloneRepoID)
	writeFile(t, filepath.Join(repo, ".sdd", "sessions", "s_kept.json"), "{}\n")
	writeFile(t, filepath.Join(repo, ".sdd", "stats", "llm.jsonl"), "{}\n")
	runApp(t, repo, "move", "--graph-repo", "https://example.test/acme/app-sdd.git")
	clone := cloneDir(t)

	for _, id := range []string{movedFirst, movedSecond} {
		if _, found := remote.Show(t, filepath.ToSlash(entryPath("graph", id))); !found {
			t.Errorf("entry %s should be published to the graph repository", id)
		}
		assertMissing(t, entryPath(filepath.Join(repo, ".sdd", "graph"), id), "the code repository should hold no entry")
	}
	if log := gitIn(t, clone, "log", "--format=%s"); !strings.Contains(log, "add the first entry") || !strings.Contains(log, "add the second entry") {
		t.Errorf("the graph repository should carry the graph's history:\n%s", log)
	}
	if session := readText(t, filepath.Join(clone, ".sdd", "sessions", "s_kept.json")); session != "{}\n" {
		t.Errorf("the in-tree session store should move beside the graph, got %q", session)
	}
	if stats := readText(t, filepath.Join(clone, ".sdd", "stats", "llm.jsonl")); stats != "{}\n" {
		t.Errorf("the usage stats should move beside the graph, got %q", stats)
	}
	codeRecord, graphRecord := headRecord(t, repo), headRecord(t, clone)
	if codeRecord.ID != graphRecord.ID || codeRecord.To.String() != "git-repo:"+string(graphCloneRepoID)+"?branch=main&dir=graph" {
		t.Errorf("both repositories should carry the same record, got %+v and %+v", codeRecord, graphRecord)
	}
	for _, path := range []string{".claude/skills/sdd/SKILL.md", ".mcp.json"} {
		if _, found := remote.Show(t, path); found {
			t.Errorf("nobody works inside sdd's own clone, so it should get no agent surfaces, found %s", path)
		}
	}
	if meta, _ := remote.Show(t, ".sdd/meta.json"); meta != movedMeta {
		t.Errorf("meta.json should reach the graph repository as it was:\n%s", meta)
	}
	if pointer := readText(t, filepath.Join(repo, ".sdd", "config.yaml")); pointer != model.FormatGraphRepoPointerConfig(model.PerRepoConfig{GraphRepo: graphCloneRepoID, SkillScope: model.ScopeProject, SupportedAgents: []model.AgentTarget{"claude"}}) {
		t.Errorf("the code repository's config should be the graph_repo pointer init writes:\n%s", pointer)
	}
	if names := sddDirContents(t, repo); !slices.Equal(names, []string{".gitignore", "config.local.yaml", "config.yaml"}) {
		t.Errorf("the code repository's .sdd should hold the pointer, its ignore file and the local layer, got %v", names)
	}
	if ignore := readText(t, filepath.Join(repo, ".gitignore")); ignore != "xdg-*/\n" {
		t.Errorf("the move should leave the root .gitignore alone:\n%s", ignore)
	}
	if local := readText(t, filepath.Join(repo, ".sdd", "config.local.yaml")); !strings.Contains(local, "participant: Ada") {
		t.Errorf("a graph_repo layout reads the machine-local config in the code repository:\n%s", local)
	}
	runApp(t, repo, "show", movedFirst)

	committedBeside := filepath.Join(repo, ".sdd", "GRAPH-ELSEWHERE.md")
	writeFile(t, committedBeside, "kept\n")
	gitIn(t, repo, "add", ".sdd/GRAPH-ELSEWHERE.md")
	gitIn(t, repo, "commit", "-q", "-m", "add a file beside the pointer")

	runApp(t, repo, "move", "--graph-dir", ".sdd/graph")
	for _, id := range []string{movedFirst, movedSecond} {
		if _, err := os.Stat(entryPath(filepath.Join(repo, ".sdd", "graph"), id)); err != nil {
			t.Errorf("entry %s should be back in the code repository: %v", id, err)
		}
		if _, found := remote.Show(t, filepath.ToSlash(entryPath("graph", id))); found {
			t.Errorf("entry %s should leave the graph repository", id)
		}
	}
	if kept := readText(t, committedBeside); kept != "kept\n" {
		t.Errorf("the move should leave a file beside the pointer as it was:\n%s", kept)
	}
	if config := readText(t, filepath.Join(repo, ".sdd", "config.yaml")); strings.Contains(config, "graph_repo") || !strings.Contains(config, "graph_dir: .sdd/graph") || !strings.Contains(config, "default_branch: main") {
		t.Errorf("the code repository should hold the project's settings again:\n%s", config)
	}
	if session := readText(t, filepath.Join(repo, ".sdd", "sessions", "s_kept.json")); session != "{}\n" {
		t.Errorf("the in-tree session store should come back, got %q", session)
	}
	runApp(t, repo, "show", movedSecond)
}

// A graph that moved on upstream since the split is not moved: nothing is
// changed, and the person pulls before running the move again.
func TestMoveRefusesWhenTheSourceMovedOnUpstream(t *testing.T) {
	repo := graphRepoToMove(t)
	bare := filepath.Join(filepath.Dir(repo), "upstream.git")
	gitIn(t, filepath.Dir(repo), "init", "-q", "--bare", bare)
	gitIn(t, repo, "remote", "add", "origin", bare)
	gitIn(t, repo, "push", "-q", "-u", "origin", "main")
	other := filepath.Join(filepath.Dir(repo), "other")
	gitIn(t, filepath.Dir(repo), "clone", "-q", bare, other)
	writeFile(t, entryPath(filepath.Join(other, ".sdd", "graph"), "20261107-100000-s-tac-far"), movedEntry("Captured elsewhere."))
	gitIn(t, other, "add", ".sdd/graph")
	gitIn(t, other, "commit", "-q", "-m", "captured elsewhere")
	gitIn(t, other, "push", "-q", "origin", "main")
	head := strings.TrimSpace(gitIn(t, repo, "rev-parse", "HEAD"))

	err := runAppResult(t, repo, "move", "--graph-dir", "graph")
	if err == nil || !strings.Contains(err.Error(), "moved on upstream") {
		t.Fatalf("a graph that moved on upstream should not move, got %v", err)
	}
	if now := strings.TrimSpace(gitIn(t, repo, "rev-parse", "HEAD")); now != head {
		t.Error("a refused move should change nothing")
	}
	if _, err := os.Stat(entryPath(filepath.Join(repo, ".sdd", "graph"), movedFirst)); err != nil {
		t.Errorf("a refused move should leave the graph where it is: %v", err)
	}
}
