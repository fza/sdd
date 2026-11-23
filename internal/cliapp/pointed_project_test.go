package cliapp_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/cliapp"
)

const pointedProjectID = "example.test/work-sdd"

// pointedRepos builds a work repository whose .sdd/ points with project_dir at
// an sdd project kept in a repository of its own.
func pointedRepos(t *testing.T) (work, project string) {
	t.Helper()
	root := canonicalTemp(t)
	work = filepath.Join(root, "work")
	project = filepath.Join(root, "work-sdd")
	for _, dir := range []string{work, filepath.Join(project, ".sdd", "graph")} {
		err := os.MkdirAll(dir, 0o755)
		if err != nil {
			t.Fatal(err)
		}
	}
	initRepo(t, work)
	initRepo(t, project)
	writeFile(t, filepath.Join(project, ".sdd", "config.yaml"), "graph_dir: .sdd/graph\nrepo_id: "+pointedProjectID+"\ndefault_branch: main\nlanguage: en\nskill_scope: project\n")
	writeFile(t, filepath.Join(project, ".sdd", "config.local.yaml"), "participant: Ada\n")
	writeFile(t, filepath.Join(work, ".sdd", "config.yaml"), "project_dir: ../work-sdd\nskill_scope: project\nsupported_agents: [claude]\n")
	return work, project
}

func runAppErr(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	return runAppAs(t, dir, "test", args...)
}

func runAppAs(t *testing.T, dir, version string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg-config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "xdg-cache"))
	t.Setenv("XDG_DATA_HOME", dataHome(dir))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "xdg-state"))
	var output bytes.Buffer
	command := cliapp.New(cliapp.Options{Version: version, Reader: strings.NewReader(""), Writer: &output, ErrWriter: &output})
	err := command.Run(t.Context(), append([]string{"sdd"}, args...))
	return output.String(), err
}

func captureSignal(t *testing.T, dir string) {
	t.Helper()
	runApp(t, dir,
		"new", "signal", "tactical",
		"The work repository carries only the work, while signals and decisions live in a project beside it.",
		"--confidence", "high", "--skip-preflight", "--skip-grounding",
		"--title", "Test title", "--summary", "A project beside the work keeps the work history free of capture commits.",
	)
}

func sddDirContents(t *testing.T, repo string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(repo, ".sdd"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// A capture run in the work repository lands in the project's graph and
// commits there, leaving the work repository's .sdd/ holding only its pointer.
func TestPointedCaptureCommitsInTheProject(t *testing.T) {
	work, project := pointedRepos(t)

	captureSignal(t, work)

	if entries := graphEntries(t, filepath.Join(project, ".sdd", "graph")); len(entries) != 1 {
		t.Fatalf("the capture should land in the project's graph, got %v", entries)
	}
	if subject := headSubject(t, project); !strings.HasPrefix(subject, "sdd: ") {
		t.Errorf("the project repository should carry the capture commit, HEAD is %q", subject)
	}
	if subject := headSubject(t, work); subject != "seed" {
		t.Errorf("the work repository should gain no commit, HEAD is %q", subject)
	}
	if names := sddDirContents(t, work); len(names) != 1 || names[0] != "config.yaml" {
		t.Errorf("the work repository's .sdd/ should keep only its config, holds %v", names)
	}
}

// The pointer runs one way: the project knows nothing of the work repository
// naming it, so someone working in the project alone needs no work repository
// on disk.
func TestProjectWorksWithoutItsWorkRepository(t *testing.T) {
	work, project := pointedRepos(t)
	captureSignal(t, work)
	err := os.RemoveAll(work)
	if err != nil {
		t.Fatal(err)
	}

	captureSignal(t, project)

	entries := graphEntries(t, filepath.Join(project, ".sdd", "graph"))
	if len(entries) != 2 {
		t.Fatalf("a capture in the project should land in its graph beside the earlier one, got %v", entries)
	}
	if subject := headSubject(t, project); !strings.HasPrefix(subject, "sdd: ") {
		t.Errorf("the project repository should carry the capture commit, HEAD is %q", subject)
	}
	parts := strings.Split(filepath.ToSlash(entries[0]), "/")
	entryID := parts[0] + parts[1] + strings.TrimSuffix(parts[2], ".md")
	for _, args := range [][]string{{"show", entryID}, {"view", "--layout", "layer(tac):as-list"}} {
		output, err := runAppErr(t, project, args...)
		if err != nil {
			t.Fatalf("sdd %s should run in the project alone: %v\n%s", args[0], err, output)
		}
		if !strings.Contains(output, entryID) {
			t.Errorf("sdd %s should read the project's graph, got:\n%s", args[0], output)
		}
	}
}

// The project's meta.json decides whether this binary may write; a pointing
// repository has none to consult.
func TestPointedWriteGateReadsTheProjectSchema(t *testing.T) {
	work, project := pointedRepos(t)
	writeFile(t, filepath.Join(project, ".sdd", "meta.json"), `{"graph_schema_version": 99}`+"\n")

	_, err := runAppAs(t, work, "1.2.3",
		"new", "signal", "tactical", "A capture against a graph from a newer schema must not be written.",
		"--confidence", "high", "--skip-preflight", "--skip-grounding", "--title", "Test title", "--summary", "Refused capture.",
	)

	if err == nil {
		t.Fatal("a capture should be refused when the project's schema is newer than the binary")
	}
	if entries := graphEntries(t, filepath.Join(project, ".sdd", "graph")); len(entries) != 0 {
		t.Errorf("a refused capture should write nothing, found %v", entries)
	}
}

func TestPointingRepositoryCarriesOnlyPointerKeys(t *testing.T) {
	cases := []struct {
		name     string
		file     string
		content  string
		mentions []string
	}{
		{
			name:     "graph fact in the committed file",
			file:     "config.yaml",
			content:  "project_dir: ../work-sdd\nrepo_id: example.test/work\n",
			mentions: []string{"config.yaml", "repo_id", "project_dir, skill_scope, supported_agents"},
		},
		{
			name:     "personal setting in the local file",
			file:     "config.local.yaml",
			content:  "participant: Ada\n",
			mentions: []string{"config.local.yaml", "participant"},
		},
		{
			name:     "agent key in the local file",
			file:     "config.local.yaml",
			content:  "project_dir: ../work-sdd\nskill_scope: user\n",
			mentions: []string{"config.local.yaml", "skill_scope"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			work, _ := pointedRepos(t)
			writeFile(t, filepath.Join(work, ".sdd", tc.file), tc.content)

			_, err := runAppErr(t, work, "config", "get", "language")

			if err == nil {
				t.Fatal("a misplaced key in a pointing repository should be refused")
			}
			for _, text := range tc.mentions {
				if !strings.Contains(err.Error(), text) {
					t.Errorf("the refusal should name %q: %v", text, err)
				}
			}
		})
	}
}

// A machine-local project_dir is how one checkout keeps the project elsewhere.
func TestLocalProjectDirOverridesTheCommittedOne(t *testing.T) {
	work, project := pointedRepos(t)
	moved := filepath.Join(filepath.Dir(project), "elsewhere")
	err := os.Rename(project, moved)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(work, ".sdd", "config.local.yaml"), "project_dir: "+moved+"\n")

	captureSignal(t, work)

	if entries := graphEntries(t, filepath.Join(moved, ".sdd", "graph")); len(entries) != 1 {
		t.Fatalf("the capture should follow the local project_dir, got %v", entries)
	}
}

func TestProjectDirMustNameAProjectThatDoesNotPointOn(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, project string)
		mention string
	}{
		{
			name: "no sdd project there",
			prepare: func(t *testing.T, project string) {
				err := os.Remove(filepath.Join(project, ".sdd", "config.yaml"))
				if err != nil {
					t.Fatal(err)
				}
			},
			mention: "clone the project there",
		},
		{
			name: "the project points on",
			prepare: func(t *testing.T, project string) {
				writeFile(t, filepath.Join(project, ".sdd", "config.yaml"), "project_dir: ../work\n")
			},
			mention: "itself sets project_dir",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			work, project := pointedRepos(t)
			tc.prepare(t, project)

			_, err := runAppErr(t, work, "config", "get", "language")

			if err == nil || !strings.Contains(err.Error(), tc.mention) {
				t.Errorf("the pointer should be refused naming the fix: %v", err)
			}
		})
	}
}

// A graph kept in another repository is a project of its own; reaching it
// through graph_dir would split commits, schema and branches across two
// repositories.
func TestGraphDirIntoAnotherRepositoryIsRefused(t *testing.T) {
	cases := []struct {
		name           string
		throughSymlink bool
		nested         bool
	}{
		{name: "path into the other repository"},
		{name: "symlink in the project leading there", throughSymlink: true},
		{name: "repository of its own inside the project", nested: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			project, sidecar := twoRepos(t)
			graphDir := filepath.Join(sidecar, "graph")
			if tc.throughSymlink {
				err := os.Symlink(graphDir, filepath.Join(project, "graph"))
				if err != nil {
					t.Fatal(err)
				}
				graphDir = "graph"
			}
			if tc.nested {
				sidecar = filepath.Join(project, "nested")
				graphDir = "nested/graph"
				err := os.MkdirAll(filepath.Join(sidecar, "graph"), 0o755)
				if err != nil {
					t.Fatal(err)
				}
				initRepo(t, sidecar)
			}
			writeFile(t, filepath.Join(project, ".sdd", "config.yaml"), "graph_dir: "+graphDir+"\nrepo_id: example.test/project\ndefault_branch: main\n")
			writeFile(t, filepath.Join(project, ".sdd", "config.local.yaml"), "participant: Ada\n")

			_, err := runAppErr(t, project,
				"new", "signal", "tactical", "A graph reached across repositories must be refused.",
				"--confidence", "high", "--skip-preflight", "--skip-grounding", "--title", "Test title", "--summary", "Refused capture.",
			)

			if err == nil || !strings.Contains(err.Error(), "project_dir") {
				t.Fatalf("a graph_dir into another repository should be refused naming project_dir: %v", err)
			}
			if entries := graphEntries(t, filepath.Join(sidecar, "graph")); len(entries) != 0 {
				t.Errorf("a refused capture should write nothing, found %v", entries)
			}
		})
	}
}

// The server started in the work repository serves the project: its identity,
// and branches of the project's repository as mutation targets.
func TestPointedServeTargetsTheProjectRepository(t *testing.T) {
	work, project := pointedRepos(t)
	gitIn(t, project, "checkout", "-q", "-b", "graph-only")
	gitIn(t, work, "checkout", "-q", "-b", "work-only")
	t.Chdir(work)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(work, "xdg-config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(work, "xdg-cache"))
	t.Setenv("XDG_DATA_HOME", dataHome(work))
	t.Setenv("XDG_STATE_HOME", filepath.Join(work, "xdg-state"))
	fixture := &appFixture{root: work}

	fixture.serve(t, func(client *mcp.ClientSession) {
		door, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "start_session", Arguments: map[string]any{}})
		if err != nil || door.IsError {
			t.Fatalf("start_session: %v %+v", err, door)
		}
		handle, _ := door.StructuredContent.(map[string]any)["session"].(string)
		if handle == "" {
			t.Fatalf("start_session served no session handle: %+v", door.StructuredContent)
		}

		info, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "info", Arguments: map[string]any{"session": handle}})
		if err != nil || info.IsError {
			t.Fatalf("info: %v %+v", err, info)
		}
		encoded, err := json.Marshal(info.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(encoded), pointedProjectID) {
			t.Errorf("the server should serve the project's identity: %s", encoded)
		}

		bound, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "bind_branch", Arguments: map[string]any{"session": handle, "branch": "graph-only"}})
		if err != nil || bound.IsError {
			t.Errorf("a branch checked out in the project repository should bind: %v %+v", err, bound)
		}
		refused, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "bind_branch", Arguments: map[string]any{"session": handle, "branch": "work-only"}})
		if err == nil && !refused.IsError {
			t.Error("a branch existing only in the work repository should not bind")
		}
	})

	if names := sddDirContents(t, work); len(names) != 1 || names[0] != "config.yaml" {
		t.Errorf("serving should leave the work repository's .sdd/ holding only its config, holds %v", names)
	}
}

func TestInitPointsTheWorkRepositoryAtTheProject(t *testing.T) {
	work, project := pointedRepos(t)
	err := os.RemoveAll(filepath.Join(work, ".sdd"))
	if err != nil {
		t.Fatal(err)
	}
	err = os.Remove(filepath.Join(project, ".sdd", "config.local.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, project, "add", ".sdd/config.yaml")
	gitIn(t, project, "commit", "-q", "-m", "chore: record the project")

	runApp(t, work, "init", "--project-dir", "../work-sdd", "--scope", "project", "--participant", "Ada", "--agents", "claude")

	written := readFile(t, filepath.Join(work, ".sdd", "config.yaml"))
	for _, key := range []string{"graph_dir:", "repo_id:", "default_branch:", "language:"} {
		if strings.Contains(written, "\n"+key) {
			t.Errorf("the pointer config should carry no project setting %s:\n%s", key, written)
		}
	}
	if !strings.Contains(written, "project_dir: ../work-sdd") {
		t.Errorf("the pointer config should name the project:\n%s", written)
	}
	if names := sddDirContents(t, work); !slices.Equal(names, []string{".gitignore", "config.yaml"}) {
		t.Errorf("init should create only the config and its .gitignore in the work repository's .sdd/, found %v", names)
	}
	if !strings.Contains(readFile(t, filepath.Join(work, ".mcp.json")), "serve") {
		t.Error("init should register the MCP server in the work repository")
	}
	if _, statErr := os.Stat(filepath.Join(work, ".claude", "skills", "sdd", "SKILL.md")); statErr != nil {
		t.Errorf("init should install the skills in the work repository: %v", statErr)
	}
	if !strings.Contains(readFile(t, filepath.Join(project, ".sdd", "config.local.yaml")), "participant: Ada") {
		t.Error("the participant should be recorded with the project")
	}
	if subject := headSubject(t, project); !strings.HasPrefix(subject, "sdd: ") {
		t.Errorf("init in the work repository should commit the project's refresh in the project, HEAD is %q", subject)
	}
	if status := gitIn(t, project, "status", "--porcelain"); status != "" {
		t.Errorf("the project's refresh should leave the project clean:\n%s", status)
	}
	if subject := headSubject(t, work); subject == "seed" {
		t.Error("init should commit the pointer and skills in the work repository")
	}
	if status := gitIn(t, work, "status", "--porcelain"); status != "" {
		t.Errorf("init should leave the work repository clean:\n%s", status)
	}

	workHead := gitIn(t, work, "rev-parse", "HEAD")
	projectHead := gitIn(t, project, "rev-parse", "HEAD")
	runApp(t, work, "init")
	if gitIn(t, project, "rev-parse", "HEAD") != projectHead {
		t.Error("a repeat init with nothing to refresh should not commit in the project")
	}
	if gitIn(t, work, "rev-parse", "HEAD") != workHead {
		t.Error("a repeat init should leave the work repository's config untouched and commit nothing")
	}
}

func TestInitRefusesAPointerItCannotHonour(t *testing.T) {
	removeWorkSDD := func(t *testing.T, work string) {
		err := os.RemoveAll(filepath.Join(work, ".sdd"))
		if err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name    string
		version string
		prepare func(t *testing.T, work, project string)
		args    []string
		mention string
	}{
		{
			name: "project pointing on, fresh work repository",
			prepare: func(t *testing.T, work, project string) {
				removeWorkSDD(t, work)
				writeFile(t, filepath.Join(project, ".sdd", "config.yaml"), "project_dir: ../work\n")
			},
			args:    []string{"init", "--project-dir", "../work-sdd", "--scope", "project", "--participant", "Ada"},
			mention: "sets project_dir itself",
		},
		{
			name: "project pointing on through its local file",
			prepare: func(t *testing.T, work, project string) {
				removeWorkSDD(t, work)
				writeFile(t, filepath.Join(project, ".sdd", "config.local.yaml"), "participant: Ada\nproject_dir: ../work\n")
			},
			args:    []string{"init", "--project-dir", "../work-sdd", "--scope", "project", "--participant", "Ada"},
			mention: "sets project_dir itself",
		},
		{
			name:    "minimum version bump from the work repository",
			version: "1.2.3",
			prepare: func(t *testing.T, work, project string) {
				removeWorkSDD(t, work)
			},
			args:    []string{"init", "--project-dir", "../work-sdd", "--bump", "--scope", "project", "--participant", "Ada"},
			mention: "run it in ../work-sdd",
		},
		{
			name:    "graph settings beside a pointer",
			args:    []string{"init", "--project-dir", "../work-sdd", "--language", "de", "--scope", "project", "--participant", "Ada"},
			mention: "--language",
		},
		{
			name: "project graph_dir reading as more than one ignore rule",
			prepare: func(t *testing.T, work, project string) {
				writeFile(t, filepath.Join(project, ".sdd", "config.yaml"), "graph_dir: \"graph\\n!secret.txt\"\nrepo_id: "+pointedProjectID+"\ndefault_branch: main\nlanguage: en\n")
			},
			args:    []string{"init", "--project-dir", "../work-sdd", "--scope", "project", "--participant", "Ada"},
			mention: "reads as more than one path",
		},
		{
			name: "work repository configured as a project",
			prepare: func(t *testing.T, work, project string) {
				writeFile(t, filepath.Join(work, ".sdd", "config.yaml"), "graph_dir: .sdd/graph\nrepo_id: example.test/work\n")
			},
			args:    []string{"init", "--project-dir", "../work-sdd", "--scope", "project", "--participant", "Ada"},
			mention: "reduce it to project_dir",
		},
		{
			name: "graph directory in another repository",
			prepare: func(t *testing.T, work, project string) {
				removeWorkSDD(t, work)
			},
			args:    []string{"init", "--graph-dir", "../work-sdd/.sdd/graph", "--language", "en", "--scope", "project", "--participant", "Ada"},
			mention: "--project-dir",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			work, project := pointedRepos(t)
			if tc.prepare != nil {
				tc.prepare(t, work, project)
			}

			version := tc.version
			if version == "" {
				version = "test"
			}

			_, err := runAppAs(t, work, version, tc.args...)

			if err == nil || !strings.Contains(err.Error(), tc.mention) {
				t.Errorf("init should be refused naming the fix: %v", err)
			}
			if subject := headSubject(t, work); subject != "seed" {
				t.Errorf("a refused init should commit nothing, HEAD is %q", subject)
			}
		})
	}
}

// A dependency is a fact about the project's graph, so declaring one from the
// work repository commits in the project.
func TestPointedRepoAddDeclaresTheDependencyInTheProject(t *testing.T) {
	work, project := pointedRepos(t)
	dependency := filepath.Join(filepath.Dir(work), "dependency")
	mkdirErr := os.MkdirAll(dependency, 0o755)
	if mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	initRepo(t, dependency)
	writeFile(t, filepath.Join(dependency, ".sdd", "config.yaml"), "repo_id: example.test/dependency\n")
	gitIn(t, dependency, "add", ".sdd/config.yaml")
	gitIn(t, dependency, "commit", "-q", "-m", "declare identity")

	output, err := runAppErr(t, work, "repo", "add", dependency)
	if err != nil {
		t.Fatalf("declaring a dependency from the work repository should commit in the project: %v\n%s", err, output)
	}

	if !strings.Contains(readFile(t, filepath.Join(project, ".sdd", "config.yaml")), "example.test/dependency") {
		t.Error("the dependency should be declared in the project's config")
	}
	if subject := headSubject(t, project); subject == "seed" {
		t.Error("the declaration should be committed in the project")
	}
	if subject := headSubject(t, work); subject != "seed" {
		t.Errorf("the work repository should gain no commit, HEAD is %q", subject)
	}
}

// `config set --local project_dir` writes the work repository's own local
// file, the one place a per-machine pointer lives, never the project's.
func TestConfigSetLocalProjectDirWritesTheWorkRepository(t *testing.T) {
	work, project := pointedRepos(t)
	moved := filepath.Join(filepath.Dir(project), "elsewhere")
	if err := os.Rename(project, moved); err != nil {
		t.Fatal(err)
	}
	projectLocal := readFile(t, filepath.Join(moved, ".sdd", "config.local.yaml"))

	output, err := runAppErr(t, work, "config", "set", "--local", "project_dir", moved)
	if err != nil {
		t.Fatalf("config set --local project_dir: %v\n%s", err, output)
	}

	if !strings.Contains(readFile(t, filepath.Join(work, ".sdd", "config.local.yaml")), "project_dir: "+moved) {
		t.Error("the pointer should land in the work repository's own local file")
	}
	if readFile(t, filepath.Join(moved, ".sdd", "config.local.yaml")) != projectLocal {
		t.Error("the project's local file should stay unchanged")
	}
	captureSignal(t, work)
	if entries := graphEntries(t, filepath.Join(moved, ".sdd", "graph")); len(entries) != 1 {
		t.Fatalf("the capture should follow the new local project_dir, got %v", entries)
	}
}

// A repository holding its own project cannot become a pointer by one local key.
func TestConfigSetLocalProjectDirIsRefusedInAProject(t *testing.T) {
	_, project := pointedRepos(t)
	local := filepath.Join(project, ".sdd", "config.local.yaml")
	before := readFile(t, local)

	_, err := runAppErr(t, project, "config", "set", "--local", "project_dir", "../other")

	if err == nil || !strings.Contains(err.Error(), "sdd init --project-dir") {
		t.Fatalf("project_dir should be refused in a project naming the way to point, got %v", err)
	}
	if readFile(t, local) != before {
		t.Error("a refused write should leave the local file unchanged")
	}
}

// A committed pointer holds no machine's path: an absolute --project-dir is
// recorded relative to the work repository.
func TestInitRecordsAnAbsoluteProjectDirRelative(t *testing.T) {
	work, project := pointedRepos(t)
	if err := os.RemoveAll(filepath.Join(work, ".sdd")); err != nil {
		t.Fatal(err)
	}

	runApp(t, work, "init", "--project-dir", project, "--scope", "project", "--participant", "Ada", "--agents", "claude")

	written := readFile(t, filepath.Join(work, ".sdd", "config.yaml"))
	if !strings.Contains(written, "project_dir: ../work-sdd\n") {
		t.Errorf("the committed pointer should be relative:\n%s", written)
	}
}

// A repeat init that follows a machine-local project_dir leaves the committed
// pointer as every clone has it.
func TestInitKeepsTheCommittedPointerUnderALocalOverride(t *testing.T) {
	work, project := pointedRepos(t)
	moved := filepath.Join(filepath.Dir(project), "elsewhere")
	if err := os.Rename(project, moved); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(work, ".sdd", "config.local.yaml"), "project_dir: "+moved+"\n")
	committed := readFile(t, filepath.Join(work, ".sdd", "config.yaml"))

	runApp(t, work, "init", "--scope", "project", "--participant", "Ada")

	if readFile(t, filepath.Join(work, ".sdd", "config.yaml")) != committed {
		t.Error("the committed pointer should stay unchanged")
	}
}

func TestGraphDirOutsideAnyRepositoryIsRefused(t *testing.T) {
	project, _ := twoRepos(t)
	outside := filepath.Join(canonicalTemp(t), "graph")
	writeFile(t, filepath.Join(project, ".sdd", "config.yaml"), "graph_dir: "+outside+"\nrepo_id: example.test/project\ndefault_branch: main\n")
	writeFile(t, filepath.Join(project, ".sdd", "config.local.yaml"), "participant: Ada\n")

	_, err := runAppErr(t, project,
		"new", "signal", "tactical", "A graph outside any repository must be refused.",
		"--confidence", "high", "--skip-preflight", "--skip-grounding", "--title", "Test title", "--summary", "Refused capture.",
	)

	if err == nil || !strings.Contains(err.Error(), "outside any repository") {
		t.Fatalf("a graph_dir outside any repository should be refused: %v", err)
	}
}

func TestProjectDirInsideTheWorkRepositoryIsRefused(t *testing.T) {
	cases := []struct {
		name     string
		repoized bool
	}{
		{name: "folder of the work repository"},
		{name: "repository of its own", repoized: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			work, project := pointedRepos(t)
			nested := filepath.Join(work, "graph-project")
			if err := os.MkdirAll(filepath.Join(nested, ".sdd", "graph"), 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(nested, ".sdd", "config.yaml"), readFile(t, filepath.Join(project, ".sdd", "config.yaml")))
			writeFile(t, filepath.Join(nested, ".sdd", "config.local.yaml"), "participant: Ada\n")
			if tc.repoized {
				initRepo(t, nested)
			}
			writeFile(t, filepath.Join(work, ".sdd", "config.local.yaml"), "project_dir: graph-project\n")

			_, err := runAppErr(t, work,
				"new", "signal", "tactical", "A project inside its work repository must be refused.",
				"--confidence", "high", "--skip-preflight", "--skip-grounding", "--title", "Test title", "--summary", "Refused capture.",
			)

			if err == nil || !strings.Contains(err.Error(), "a repository of its own") {
				t.Fatalf("a project_dir inside the work repository should be refused: %v", err)
			}
			if entries := graphEntries(t, filepath.Join(nested, ".sdd", "graph")); len(entries) != 0 {
				t.Errorf("a refused capture should write nothing, found %v", entries)
			}
		})
	}
}

func TestProjectDirInsideAnotherRepositoryIsRefused(t *testing.T) {
	work, project := pointedRepos(t)
	mono := filepath.Join(filepath.Dir(work), "mono")
	sub := filepath.Join(mono, "sub")
	if err := os.MkdirAll(filepath.Join(sub, ".sdd", "graph"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sub, ".sdd", "config.yaml"), readFile(t, filepath.Join(project, ".sdd", "config.yaml")))
	writeFile(t, filepath.Join(sub, ".sdd", "config.local.yaml"), "participant: Ada\n")
	initRepo(t, mono)
	writeFile(t, filepath.Join(work, ".sdd", "config.local.yaml"), "project_dir: ../mono/sub\n")

	_, err := runAppErr(t, work,
		"new", "signal", "tactical", "A project inside another repository must be refused.",
		"--confidence", "high", "--skip-preflight", "--skip-grounding", "--title", "Test title", "--summary", "Refused capture.",
	)

	if err == nil || !strings.Contains(err.Error(), "lies inside the repository") {
		t.Fatalf("a project_dir inside another repository should be refused: %v", err)
	}
	if entries := graphEntries(t, filepath.Join(sub, ".sdd", "graph")); len(entries) != 0 {
		t.Errorf("a refused capture should write nothing, found %v", entries)
	}
	if subject := headSubject(t, mono); subject != "seed" {
		t.Errorf("a refused capture should commit nothing in the other repository, HEAD is %q", subject)
	}
}

// git exports GIT_DIR to hooks; sdd run from one in the work repository still
// reaches the project's repository.
func TestPointedCaptureUnderAnInheritedGitDir(t *testing.T) {
	cases := []struct {
		name   string
		gitDir func(work string) string
	}{
		{name: "absolute", gitDir: func(work string) string { return filepath.Join(work, ".git") }},
		{name: "relative", gitDir: func(string) string { return ".git" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			work, project := pointedRepos(t)
			t.Setenv("GIT_DIR", tc.gitDir(work))

			_, err := runAppErr(t, work,
				"new", "signal", "tactical", "A capture from a git hook lands in the project.",
				"--confidence", "high", "--skip-preflight", "--skip-grounding", "--title", "Test title", "--summary", "Capture under GIT_DIR.",
			)
			if unsetErr := os.Unsetenv("GIT_DIR"); unsetErr != nil {
				t.Fatal(unsetErr)
			}

			if err != nil {
				t.Fatalf("a capture under an inherited GIT_DIR should succeed: %v", err)
			}
			if subject := headSubject(t, project); !strings.HasPrefix(subject, "sdd: ") {
				t.Errorf("the capture should commit in the project, HEAD is %q", subject)
			}
			if subject := headSubject(t, work); subject != "seed" {
				t.Errorf("the work repository should gain no commit, HEAD is %q", subject)
			}
		})
	}
}

func TestProjectDirOutsideAnyRepositoryIsRefused(t *testing.T) {
	work, project := pointedRepos(t)
	loose := filepath.Join(filepath.Dir(work), "loose")
	if err := os.MkdirAll(filepath.Join(loose, ".sdd", "graph"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(loose, ".sdd", "config.yaml"), readFile(t, filepath.Join(project, ".sdd", "config.yaml")))
	writeFile(t, filepath.Join(loose, ".sdd", "config.local.yaml"), "participant: Ada\n")
	writeFile(t, filepath.Join(work, ".sdd", "config.local.yaml"), "project_dir: ../loose\n")

	_, err := runAppErr(t, work,
		"new", "signal", "tactical", "A project outside any repository must be refused.",
		"--confidence", "high", "--skip-preflight", "--skip-grounding", "--title", "Test title", "--summary", "Refused capture.",
	)

	if err == nil || !strings.Contains(err.Error(), "outside any repository") {
		t.Fatalf("a project_dir outside any repository should be refused: %v", err)
	}
	if entries := graphEntries(t, filepath.Join(loose, ".sdd", "graph")); len(entries) != 0 {
		t.Errorf("a refused capture should write nothing, found %v", entries)
	}
}

func TestProjectDirContainingTheWorkRepositoryIsRefused(t *testing.T) {
	cases := []struct {
		name     string
		repoized bool
	}{
		{name: "folder outside any repository"},
		{name: "repository holding the work repository", repoized: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			work, project := pointedRepos(t)
			parent := filepath.Dir(work)
			if err := os.MkdirAll(filepath.Join(parent, ".sdd", "graph"), 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(parent, ".sdd", "config.yaml"), readFile(t, filepath.Join(project, ".sdd", "config.yaml")))
			writeFile(t, filepath.Join(parent, ".sdd", "config.local.yaml"), "participant: Ada\n")
			if tc.repoized {
				initRepo(t, parent)
			}
			writeFile(t, filepath.Join(work, ".sdd", "config.local.yaml"), "project_dir: ..\n")

			_, err := runAppErr(t, work,
				"new", "signal", "tactical", "A project holding its work repository must be refused.",
				"--confidence", "high", "--skip-preflight", "--skip-grounding", "--title", "Test title", "--summary", "Refused capture.",
			)

			if err == nil || !strings.Contains(err.Error(), "contains the work repository") {
				t.Fatalf("a project_dir containing the work repository should be refused: %v", err)
			}
			if entries := graphEntries(t, filepath.Join(parent, ".sdd", "graph")); len(entries) != 0 {
				t.Errorf("a refused capture should write nothing, found %v", entries)
			}
		})
	}
}

// A typed absolute path spelled through a symlink still records the short
// relative pointer the real folders give.
func TestInitRecordsAnAbsoluteProjectDirThroughASymlinkRelative(t *testing.T) {
	work, project := pointedRepos(t)
	if err := os.RemoveAll(filepath.Join(work, ".sdd")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(filepath.Dir(project), link); err != nil {
		t.Fatal(err)
	}

	runApp(t, work, "init", "--project-dir", filepath.Join(link, "work-sdd"), "--scope", "project", "--participant", "Ada", "--agents", "claude")

	written := readFile(t, filepath.Join(work, ".sdd", "config.yaml"))
	if !strings.Contains(written, "project_dir: ../work-sdd\n") {
		t.Errorf("the committed pointer should be relative to the real folders:\n%s", written)
	}
}

// A typed absolute path beyond the repository's parent would commit one
// machine's layout, so it belongs in the local file.
func TestInitRefusesToCommitAFarAbsoluteProjectDir(t *testing.T) {
	work, _ := pointedRepos(t)
	if err := os.RemoveAll(filepath.Join(work, ".sdd")); err != nil {
		t.Fatal(err)
	}
	far := filepath.Join(canonicalTemp(t), "deep", "far-sdd")
	if err := os.MkdirAll(filepath.Join(far, ".sdd"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(far, ".sdd", "config.yaml"), "graph_dir: .sdd/graph\nrepo_id: example.test/far\ndefault_branch: main\nlanguage: en\n")
	initRepo(t, far)

	_, err := runAppErr(t, work, "init", "--project-dir", far, "--scope", "project", "--participant", "Ada", "--agents", "claude")

	if err == nil || !strings.Contains(err.Error(), "config.local.yaml") {
		t.Fatalf("a far absolute project_dir should be refused naming the local file, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(work, ".sdd")); !os.IsNotExist(statErr) {
		t.Errorf("a refused init should write nothing in the work repository, stat err = %v", statErr)
	}
}
