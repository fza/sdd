package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/index"
	"github.com/networkteam/sdd/internal/model"
	localadapter "github.com/networkteam/sdd/pkg/local"
	"github.com/networkteam/sdd/test"
)

// projectCheckout is an existing repository at graph-home beside repo,
// holding files of its own committed.
func projectCheckout(t *testing.T, repo string, files map[string]string) string {
	t.Helper()
	project := filepath.Join(filepath.Dir(repo), "graph-home")
	err := os.MkdirAll(project, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	initRepo(t, project)
	for name, content := range files {
		writeFile(t, filepath.Join(project, filepath.FromSlash(name)), content)
	}
	gitIn(t, project, "add", ".")
	gitIn(t, project, "commit", "-q", "-m", "the checkout's own files")
	return project
}

const otherServer = "{\n  \"mcpServers\": {\n    \"other\": {\n      \"command\": \"x\"\n    }\n  }\n}\n"

// The install step merges into what the checkout holds and leaves a skill
// sdd did not write alone; the move back removes only what sdd added.
func TestMoveKeepsWhatTheCheckoutHoldsBeyondSdd(t *testing.T) {
	repo := graphRepoToMove(t)
	const edited = "---\nname: sdd\n---\nThe checkout's own skill.\n"
	project := projectCheckout(t, repo, map[string]string{".mcp.json": otherServer, ".claude/skills/sdd/SKILL.md": edited})
	runApp(t, repo, "move", "--project-dir", "../graph-home")

	if registration := readText(t, filepath.Join(project, ".mcp.json")); !strings.Contains(registration, "\"other\"") || !strings.Contains(registration, "\"sdd\"") {
		t.Errorf("the registration should join the checkout's own servers:\n%s", registration)
	}
	if skill := readText(t, filepath.Join(project, ".claude", "skills", "sdd", "SKILL.md")); skill != edited {
		t.Errorf("a skill sdd did not write should be left as init leaves it:\n%s", skill)
	}

	gitIn(t, project, "rm", "-q", ".claude/skills/sdd/SKILL.md")
	gitIn(t, project, "commit", "-q", "-m", "drop the checkout's own skill")
	runApp(t, repo, "move", "--graph-dir", ".sdd/graph")
	if registration := readText(t, filepath.Join(project, ".mcp.json")); registration != otherServer {
		t.Errorf("the move back should remove only the sdd registration:\n%s", registration)
	}
	assertMissing(t, filepath.Join(project, ".claude"), "the move back removes every skill sdd wrote")
}

// A skill an earlier build wrote that the bundle no longer carries goes with
// the rest from the checkout the graph leaves, unless it was edited.
func TestMoveRemovesTheOrphanedSkillsOfAVacatedCheckout(t *testing.T) {
	orphan := func(t *testing.T) []byte {
		t.Helper()
		entry := model.SkillBundleEntry{Skill: "sdd", RelPath: "references/gone.md", Content: []byte("---\nname: gone\n---\nA reference the bundle dropped.\n")}
		rendered, err := model.RenderSkillFile(entry, model.AgentClaude, "v0.1.0", model.ComputeSkillHash(entry.Content))
		if err != nil {
			t.Fatal(err)
		}
		return rendered
	}
	setUp := func(t *testing.T, content []byte) (repo, project, path string) {
		t.Helper()
		repo = graphRepoToMove(t)
		project = filepath.Join(filepath.Dir(repo), "graph-home")
		runApp(t, repo, "move", "--project-dir", "../graph-home")
		path = filepath.Join(project, ".claude", "skills", "sdd", "references", "gone.md")
		writeFile(t, path, string(content))
		gitIn(t, project, "add", ".claude")
		gitIn(t, project, "commit", "-q", "-m", "a skill file of an earlier build")
		return repo, project, path
	}
	t.Run("unedited", func(t *testing.T) {
		repo, project, path := setUp(t, orphan(t))
		runApp(t, repo, "move", "--graph-dir", ".sdd/graph")
		assertMissing(t, path, "an orphaned skill sdd wrote should go")
		assertMissing(t, filepath.Join(project, ".claude"), "the vacated checkout should keep no skill")
	})
	t.Run("edited", func(t *testing.T) {
		repo, _, path := setUp(t, append(orphan(t), []byte("An edit of our own.\n")...))
		err := runAppResult(t, repo, "move", "--graph-dir", ".sdd/graph")
		if err == nil || !strings.Contains(err.Error(), path) {
			t.Fatalf("an edited orphan should refuse the move, naming the file, got %v", err)
		}
	})
}

// Skills under user scope live outside both repositories: the move installs
// and removes none of them.
func TestMoveLeavesUserScopeSkillsAlone(t *testing.T) {
	repo := graphRepoToMove(t)
	project := filepath.Join(filepath.Dir(repo), "graph-home")
	configPath := filepath.Join(repo, ".sdd", "config.yaml")
	writeFile(t, configPath, strings.Replace(readText(t, configPath), "skill_scope: project", "skill_scope: user", 1))
	gitIn(t, repo, "commit", "-q", "-am", "skills under user scope")

	runApp(t, repo, "move", "--project-dir", "../graph-home")
	for _, path := range []string{".claude", ".mcp.json"} {
		assertMissing(t, filepath.Join(project, path), "a user-scope layout installs nothing in the project")
	}
	runApp(t, repo, "move", "--graph-dir", ".sdd/graph")
	if status := strings.TrimSpace(gitIn(t, project, "status", "--porcelain")); status != "" {
		t.Errorf("the move back should leave the vacated checkout clean:\n%s", status)
	}
}

// A graph keeping a repo_id finds its sessions and its search index under
// the identity it had before the move, read before the move rewrites its
// config.
func TestMoveCarriesTheStoresOfAGraphWithARepoID(t *testing.T) {
	repo := graphRepoToMove(t)
	const repoID = "example.test/acme/app"
	configPath := filepath.Join(repo, ".sdd", "config.yaml")
	writeFile(t, configPath, readText(t, configPath)+"repo_id: "+repoID+"\n")
	gitIn(t, repo, "commit", "-q", "-am", "record the repository identity")
	test.NewGraphRemote(t, graphCloneRepoID)
	t.Setenv("XDG_STATE_HOME", filepath.Join(repo, "xdg-state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(repo, "xdg-cache"))
	locations := machineLocations(t)
	before, err := localadapter.SessionLocations(locations.StateRoot, filepath.Join(repo, ".sdd"), repoID, stableRoot(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(before[0].Sessions, "s_open.jsonl"), "{}\n")
	store := index.StoreDir(locations.CacheRoot, repoID, "embedder")
	writeFile(t, filepath.Join(store, "manifest.json"), "{}\n")

	runApp(t, repo, "move", "--graph-repo", "https://example.test/acme/app-sdd.git")

	clone := filepath.Join(dataHome(repo), "sdd", "repos", filepath.FromSlash(string(graphCloneRepoID)))
	after, err := localadapter.SessionLocations(locations.StateRoot, filepath.Join(clone, ".sdd"), graphCloneRepoID, stableRoot(t, clone))
	if err != nil {
		t.Fatal(err)
	}
	if session := readText(t, filepath.Join(after[0].Sessions, "s_open.jsonl")); session != "{}\n" {
		t.Errorf("the session should be found under the graph repository's identity, got %q", session)
	}
	if index.RealStoreDir(index.StoreDir(locations.CacheRoot, string(graphCloneRepoID), "embedder")) != index.RealStoreDir(store) {
		t.Error("the graph repository's identity should reach the search index store the graph had")
	}
}
