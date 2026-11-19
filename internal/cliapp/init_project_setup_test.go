package cliapp_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/test"
)

// isolateGitIdentity gives the repositories init creates itself an author,
// with no global or system git config reaching the test.
func isolateGitIdentity(t *testing.T, root string) {
	t.Helper()
	globalConfig := filepath.Join(root, "gitconfig")
	writeFile(t, globalConfig, "")
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
}

// workRepository builds a work repository with no sdd setup.
func workRepository(t *testing.T) string {
	t.Helper()
	root := canonicalTemp(t)
	isolateGitIdentity(t, root)
	work := filepath.Join(root, "work")
	err := os.MkdirAll(work, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	initRepo(t, work)
	return work
}

// Git limits live in the user-global config only, so init asks nothing
// about sync and writes no sync key.
func TestInitWritesNoSyncSettings(t *testing.T) {
	work := workRepository(t)

	runApp(t, work, "init", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude")

	for _, name := range []string{"config.yaml", "config.local.yaml"} {
		if regexp.MustCompile(`(?m)^sync:`).MatchString(readFile(t, filepath.Join(work, ".sdd", name))) {
			t.Errorf("init should write no sync key into %s", name)
		}
	}
	_, err := runAppErr(t, work, "init", "--sync-mode", "manual")
	if err == nil || !strings.Contains(err.Error(), "sync-mode") {
		t.Errorf("init should no longer take a sync mode: %v", err)
	}
}

func TestInitRefusesAGraphDirReadingAsIgnoreRules(t *testing.T) {
	work := workRepository(t)

	_, err := runAppErr(t, work, "init", "--graph-dir", "!graph", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude")

	if err == nil || !strings.Contains(err.Error(), "reads as more than one path") {
		t.Errorf("init should refuse a graph directory that negates an ignore rule: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(work, ".sdd")); statErr == nil {
		t.Error("a refused init should write nothing")
	}
}

// A repository holding only the graph sets it up under graph/ when no
// --graph-dir is given; one holding code beside it keeps .sdd/graph, and an
// existing project keeps the graph_dir it records.
func TestInitDefaultsTheGraphDirToTheLayout(t *testing.T) {
	cases := []struct {
		name string
		// initGraphProject runs init and returns the root of the repository
		// holding the graph.
		initGraphProject func(t *testing.T) string
		graphDir         string
		otherGraphDir    string
	}{
		{
			name: "graph repository set up through graph_repo",
			initGraphProject: func(t *testing.T) string {
				test.NewGraphRemote(t, graphCloneRepoID)
				code := filepath.Join(canonicalTemp(t), "code")
				err := os.MkdirAll(code, 0o755)
				if err != nil {
					t.Fatal(err)
				}
				initRepo(t, code)
				runApp(t, code, "init", "--graph-repo", "https://example.test/acme/app-sdd.git", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude")
				return cloneDir(t)
			},
			graphDir:      "graph",
			otherGraphDir: ".sdd/graph",
		},
		{
			name: "single repository",
			initGraphProject: func(t *testing.T) string {
				work := workRepository(t)
				runApp(t, work, "init", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude")
				return work
			},
			graphDir:      ".sdd/graph",
			otherGraphDir: "graph",
		},
		{
			name: "existing project recording its graph directory",
			initGraphProject: func(t *testing.T) string {
				work := workRepository(t)
				runApp(t, work, "init", "--graph-dir", "decisions", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude")
				runApp(t, work, "init")
				return work
			},
			graphDir:      "decisions",
			otherGraphDir: ".sdd/graph",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := tc.initGraphProject(t)

			hasLine := func(content, line string) bool {
				return slices.Contains(strings.Split(content, "\n"), line)
			}
			config := readFile(t, filepath.Join(root, ".sdd", "config.yaml"))
			if !hasLine(config, "graph_dir: "+tc.graphDir) {
				t.Errorf("the graph project's config should record the graph directory fitting its layout:\n%s", config)
			}
			gitattributes := readFile(t, filepath.Join(root, filepath.FromSlash(tc.graphDir), ".gitattributes"))
			if !hasLine(gitattributes, "[0-9][0-9][0-9][0-9]/[0-9][0-9]/[0-9][0-9]-*.md merge=sdd-entry") {
				t.Errorf("that graph directory's .gitattributes should route its entries to the merge driver:\n%s", gitattributes)
			}
			if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(tc.otherGraphDir))); statErr == nil {
				t.Error("init should set up no graph directory beside the one fitting the layout")
			}
		})
	}
}
