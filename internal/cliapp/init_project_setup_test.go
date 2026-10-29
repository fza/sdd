package cliapp_test

import (
	"os"
	"path/filepath"
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

			runApp(t, work, "init", "--project-dir", "../work-sdd", "--graph-dir", "graph", "--language", "de", "--scope", "project", "--participant", "Ada", "--agents", "claude")

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
			if !strings.Contains(readFile(t, filepath.Join(project, ".sdd", "config.local.yaml")), "participant: Ada") {
				t.Error("the participant should be recorded with the project")
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
			name:       "folder inside the work repository",
			projectDir: "graph-project",
			args:       []string{"--language", "en"},
			mention:    "lies inside the repository",
		},
		{
			name:       "work repository itself",
			projectDir: ".",
			args:       []string{"--language", "en"},
			mention:    "lies inside the repository",
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

			args := append([]string{"init", "--project-dir", tc.projectDir, "--scope", "project", "--participant", "Ada"}, tc.args...)
			_, err := runAppErr(t, work, args...)

			if err == nil || !strings.Contains(err.Error(), tc.mention) {
				t.Errorf("the setup should be refused naming the fix: %v", err)
			}
			if subject := headSubject(t, work); subject != "seed" {
				t.Errorf("a refused setup should commit nothing in the work repository, HEAD is %q", subject)
			}
			if _, statErr := os.Stat(filepath.Join(projectRoot, ".sdd")); statErr == nil {
				t.Error("a refused setup should write no project")
			}
		})
	}
}
