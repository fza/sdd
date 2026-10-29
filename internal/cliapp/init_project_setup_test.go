package cliapp_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
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

// workRepository builds a work repository with no sdd setup beside a folder
// for its project.
func workRepository(t *testing.T) (work, project string) {
	t.Helper()
	root := canonicalTemp(t)
	isolateGitIdentity(t, root)
	work = filepath.Join(root, "work")
	project = filepath.Join(root, "work-sdd")
	err := os.MkdirAll(work, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	initRepo(t, work)
	return work, project
}

func TestInitSetsUpAMissingProject(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, project string)
		repoID  string
	}{
		{
			name: "folder missing",
		},
		{
			name: "empty folder outside any repository",
			prepare: func(t *testing.T, project string) {
				err := os.MkdirAll(project, 0o755)
				if err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "empty clone",
			prepare: func(t *testing.T, project string) {
				err := os.MkdirAll(project, 0o755)
				if err != nil {
					t.Fatal(err)
				}
				gitIn(t, project, "init", "-q")
				gitIn(t, project, "remote", "add", "origin", "https://example.test/team/work-sdd.git")
			},
			repoID: "example.test/team/work-sdd",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			work, project := workRepository(t)
			if tc.prepare != nil {
				tc.prepare(t, project)
			}

			runApp(t, work, "init", "--project-dir", "../work-sdd", "--graph-dir", "graph", "--language", "de", "--scope", "project", "--participant", "Ada", "--agents", "claude", "--sync-mode", "auto", "--sync-interval", "5m")

			projectConfig := readFile(t, filepath.Join(project, ".sdd", "config.yaml"))
			for _, setting := range []string{"graph_dir: graph", "language: de", "skill_scope: project"} {
				if !strings.Contains(projectConfig, setting) {
					t.Errorf("the project config should carry %q from the flags:\n%s", setting, projectConfig)
				}
			}
			if tc.repoID != "" && !strings.Contains(projectConfig, "repo_id: "+tc.repoID) {
				t.Errorf("the project's remote should derive its repo_id:\n%s", projectConfig)
			}
			if _, statErr := os.Stat(filepath.Join(project, "graph")); statErr != nil {
				t.Errorf("the project should hold its graph directory: %v", statErr)
			}
			if _, statErr := os.Stat(filepath.Join(project, ".sdd", "meta.json")); statErr != nil {
				t.Errorf("the project should hold its meta.json: %v", statErr)
			}
			projectLocal := readFile(t, filepath.Join(project, ".sdd", "config.local.yaml"))
			for _, setting := range []string{"participant: Ada", "mode: auto", "interval: 5m"} {
				if !strings.Contains(projectLocal, setting) {
					t.Errorf("the person's settings should be recorded with the project, missing %q:\n%s", setting, projectLocal)
				}
			}
			if _, statErr := os.Stat(filepath.Join(work, ".sdd", "config.local.yaml")); statErr == nil {
				t.Error("the work repository should receive no per-person settings")
			}
			projectIgnore := readFile(t, filepath.Join(project, ".gitignore"))
			workIgnore := readFile(t, filepath.Join(work, ".gitignore"))
			if projectIgnore != workIgnore || !strings.Contains(workIgnore, "graph/.sdd-runtime/") {
				t.Errorf("both repositories should ignore the same runtime paths:\nproject:\n%s\nwork:\n%s", projectIgnore, workIgnore)
			}
			if !strings.HasPrefix(headSubject(t, project), "sdd: ") {
				t.Error("the project's setup should be committed in its own repository")
			}
			if status := gitIn(t, project, "status", "--porcelain"); status != "" {
				t.Errorf("the project's setup should leave its repository clean:\n%s", status)
			}

			workConfig := readFile(t, filepath.Join(work, ".sdd", "config.yaml"))
			if !strings.Contains(workConfig, "project_dir: ../work-sdd") || strings.Contains(workConfig, "graph_dir:") {
				t.Errorf("the work repository should carry only the pointer:\n%s", workConfig)
			}
			if headSubject(t, work) == "seed" {
				t.Error("the pointer should be committed in the work repository")
			}
			if status := gitIn(t, work, "status", "--porcelain"); status != "" {
				t.Errorf("init should leave the work repository clean:\n%s", status)
			}

			captureSignal(t, work)
			if entries := graphEntries(t, filepath.Join(project, "graph")); len(entries) != 1 {
				t.Errorf("a capture from the work repository should land in the set-up project's graph, got %v", entries)
			}
		})
	}
}

// A refused setup leaves both sides as they were, so nothing is half set up.
func TestInitRefusesAProjectSetupItCannotHonour(t *testing.T) {
	cases := []struct {
		name       string
		projectDir string
		recorded   bool
		prepare    func(t *testing.T, work, project string)
		args       []string
		mention    string
	}{
		{
			name:       "language missing without a terminal",
			projectDir: "../work-sdd",
			mention:    "--language",
		},
		{
			name:       "recorded pointer at a project not cloned",
			projectDir: "../work-sdd",
			recorded:   true,
			prepare: func(t *testing.T, work, project string) {
				writeFile(t, filepath.Join(work, ".sdd", "config.yaml"), "project_dir: ../work-sdd\nskill_scope: project\nsupported_agents: [claude]\n")
			},
			args:    []string{"--language", "en"},
			mention: "clone the project there",
		},
		{
			name:       "folder inside the work repository",
			projectDir: "graph-project",
			args:       []string{"--language", "en"},
			mention:    "lies inside this repository",
		},
		{
			name:       "work repository itself",
			projectDir: ".",
			args:       []string{"--language", "en"},
			mention:    "lies inside this repository",
		},
		{
			name:       "repository of its own nested in the work repository",
			projectDir: "nested-project",
			prepare: func(t *testing.T, work, project string) {
				nested := filepath.Join(work, "nested-project")
				err := os.MkdirAll(nested, 0o755)
				if err != nil {
					t.Fatal(err)
				}
				initRepo(t, nested)
			},
			args:    []string{"--language", "en"},
			mention: "lies inside this repository",
		},
		{
			name:       "graph directory leading out of the project",
			projectDir: "../work-sdd",
			args:       []string{"--language", "en", "--graph-dir", "../elsewhere"},
			mention:    "leads out of the project",
		},
		{
			name:       "project folder with a .sdd/ but no config",
			projectDir: "../work-sdd",
			prepare: func(t *testing.T, work, project string) {
				writeFile(t, filepath.Join(project, ".sdd", "config.local.yaml"), "participant: Ada\n")
			},
			args:    []string{"--language", "en"},
			mention: "without config.yaml",
		},
		{
			name:       "folder containing the work repository",
			projectDir: "..",
			args:       []string{"--language", "en"},
			mention:    "contains this repository",
		},
		{
			name:       "graph directory reading as an ignore negation",
			projectDir: "../work-sdd",
			args:       []string{"--language", "en", "--graph-dir", "!graph"},
			mention:    "reads as more than one path",
		},
		{
			name:       "unknown sync mode",
			projectDir: "../work-sdd",
			args:       []string{"--language", "en", "--sync-mode", "sometimes"},
			mention:    "sync.mode",
		},
		{
			name:       "folder inside another repository",
			projectDir: "../work-sdd/nested",
			prepare: func(t *testing.T, work, project string) {
				err := os.MkdirAll(project, 0o755)
				if err != nil {
					t.Fatal(err)
				}
				initRepo(t, project)
			},
			args:    []string{"--language", "en"},
			mention: "lies inside the repository",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			work, project := workRepository(t)
			if tc.prepare != nil {
				tc.prepare(t, work, project)
			}
			projectRoot := filepath.Join(work, tc.projectDir)

			args := []string{"init", "--scope", "project", "--participant", "Ada"}
			if !tc.recorded {
				args = append(args, "--project-dir", tc.projectDir)
			}
			args = append(args, tc.args...)
			workHead := gitIn(t, work, "rev-parse", "HEAD")
			workStatus := gitIn(t, work, "status", "--porcelain", "--untracked-files=all")
			_, projectExisted := os.Stat(projectRoot)

			_, err := runAppErr(t, work, args...)

			if err == nil || !strings.Contains(err.Error(), tc.mention) {
				t.Errorf("the setup should be refused naming the fix: %v", err)
			}
			if gitIn(t, work, "rev-parse", "HEAD") != workHead {
				t.Error("a refused setup should commit nothing in the work repository")
			}
			if gitIn(t, work, "status", "--porcelain", "--untracked-files=all") != workStatus {
				t.Error("a refused setup should leave the work repository's files as they were")
			}
			if _, statErr := os.Stat(filepath.Join(projectRoot, ".sdd", "config.yaml")); statErr == nil {
				t.Error("a refused setup should write no project")
			}
			if _, statErr := os.Stat(projectRoot); projectExisted != nil && statErr == nil {
				t.Error("a refused setup should create no project folder")
			}
		})
	}
}

// An init in the work repository runs the project's own init too, with the
// settings the project records (20261029-132740-d-tac-g32).
func TestInitRefreshesTheProjectFromTheWorkRepository(t *testing.T) {
	work, project := workRepository(t)
	runApp(t, work, "init", "--project-dir", "../work-sdd", "--graph-dir", "graph", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude")
	skill := filepath.Join(project, ".claude", "skills", "sdd", "SKILL.md")
	gitIn(t, project, "rm", "-q", skill)
	gitIn(t, project, "commit", "-q", "-m", "chore: drop the skill")
	workHead := gitIn(t, work, "rev-parse", "HEAD")

	runApp(t, work, "init")

	if _, statErr := os.Stat(skill); statErr != nil {
		t.Errorf("the project's skills should be refreshed from the work repository: %v", statErr)
	}
	if !strings.HasPrefix(headSubject(t, project), "sdd: ") {
		t.Error("the project's refresh should be committed in the project repository")
	}
	if status := gitIn(t, project, "status", "--porcelain"); status != "" {
		t.Errorf("the refresh should leave the project clean:\n%s", status)
	}
	if _, statErr := os.Stat(filepath.Join(project, ".sdd", "graph")); statErr == nil {
		t.Error("the refresh should keep the project's own graph_dir")
	}
	if gitIn(t, work, "rev-parse", "HEAD") != workHead {
		t.Error("the work repository had nothing to refresh and should gain no commit")
	}

	projectHead := gitIn(t, project, "rev-parse", "HEAD")
	runApp(t, work, "init")
	if gitIn(t, project, "rev-parse", "HEAD") != projectHead || gitIn(t, work, "rev-parse", "HEAD") != workHead {
		t.Error("a repeat init with nothing to refresh should commit in neither repository")
	}
}

// Graph sync is set per person, in the config.local.yaml of the repository
// holding the graph (20261027-211908-d-tac-l8j).
func TestInitRecordsGraphSyncSettings(t *testing.T) {
	work, _ := workRepository(t)

	runApp(t, work, "init", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude", "--sync-mode", "manual")

	local := readFile(t, filepath.Join(work, ".sdd", "config.local.yaml"))
	if !strings.Contains(local, "mode: manual") {
		t.Errorf("an explicit manual sync should be recorded so it is not asked again:\n%s", local)
	}
	if regexp.MustCompile(`(?m)^\s*mode:`).MatchString(readFile(t, filepath.Join(work, ".sdd", "config.yaml"))) {
		t.Error("the sync mode should stay out of the committed config")
	}
}

// A project recording its default branch needs no branch checked out to be
// refreshed.
func TestInitRefreshesAProjectOnADetachedHead(t *testing.T) {
	work, project := pointedRepos(t)
	gitIn(t, project, "add", ".sdd/config.yaml")
	gitIn(t, project, "commit", "-q", "-m", "chore: record the project")
	gitIn(t, project, "checkout", "-q", "--detach")

	_, err := runAppErr(t, work, "init")

	if err != nil {
		t.Errorf("a detached HEAD in the project should not block its refresh: %v", err)
	}
}

// A project path spelled through a symlinked prefix, as macOS's /var is,
// resolves like the canonical one.
func TestInitSetsUpAProjectAtAnAbsolutePathThroughASymlink(t *testing.T) {
	work, _ := workRepository(t)
	project := filepath.Join(t.TempDir(), "work-sdd")

	runApp(t, work, "init", "--project-dir", project, "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude")

	if _, statErr := os.Stat(filepath.Join(project, ".sdd", "config.yaml")); statErr != nil {
		t.Errorf("the project should be set up at the absolute path: %v", statErr)
	}
}

func TestInitRefusesAGraphDirReadingAsIgnoreRules(t *testing.T) {
	work, _ := workRepository(t)

	_, err := runAppErr(t, work, "init", "--graph-dir", "!graph", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude")

	if err == nil || !strings.Contains(err.Error(), "reads as more than one path") {
		t.Errorf("init should refuse a graph directory that negates an ignore rule: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(work, ".sdd")); statErr == nil {
		t.Error("a refused init should write nothing")
	}
}

// The project's refresh runs with the scope the project records, not the
// work repository's, falling back to the default as an init inside the
// project would.
func TestInitRefreshKeepsTheWorkRepositoryScopeOut(t *testing.T) {
	work, project := pointedRepos(t)
	home := filepath.Join(filepath.Dir(work), "home")
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(project, ".sdd", "config.yaml"), "graph_dir: .sdd/graph\nrepo_id: "+pointedProjectID+"\ndefault_branch: main\nlanguage: en\n")
	gitIn(t, project, "add", ".sdd/config.yaml")
	gitIn(t, project, "commit", "-q", "-m", "chore: record the project")

	runApp(t, work, "init", "--scope", "project", "--participant", "Ada")

	projectConfig := readFile(t, filepath.Join(project, ".sdd", "config.yaml"))
	if strings.Contains(projectConfig, "skill_scope: project") {
		t.Errorf("the work repository's --scope should not reach the project's config:\n%s", projectConfig)
	}
	if _, statErr := os.Stat(filepath.Join(project, ".claude", "skills")); statErr == nil {
		t.Error("the project should not receive project-scope skills it never chose")
	}
}
