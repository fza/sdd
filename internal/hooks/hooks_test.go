package hooks_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/hooks"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

// world is a user-global config folder and one repository, each with a
// hooks folder.
type world struct {
	global string
	repo   string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	base := t.TempDir()
	w := &world{global: filepath.Join(base, "config", "sdd", "config.yaml"), repo: filepath.Join(base, "repo")}
	write(t, w.global, "")
	write(t, filepath.Join(w.repo, ".sdd", "config.yaml"), "repo_id: example.com/org/repo\n")
	return w
}

func write(t *testing.T, path, content string) {
	t.Helper()
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(path, []byte(content), 0o644)
	if err != nil {
		t.Fatal(err)
	}
}

func script(t *testing.T, path, body string) {
	t.Helper()
	write(t, path, "#!/bin/sh\n"+body+"\n")
	err := os.Chmod(path, 0o755)
	if err != nil {
		t.Fatal(err)
	}
}

func (w *world) hooksDir() string { return filepath.Join(w.repo, ".sdd", "hooks") }

func (w *world) commit(t *testing.T, hooksYAML string) {
	t.Helper()
	write(t, filepath.Join(w.repo, ".sdd", "config.yaml"), "repo_id: example.com/org/repo\nhooks:\n"+hooksYAML)
}

func (w *world) sources() hooks.Sources {
	return hooks.Sources{GlobalConfig: w.global, Project: meta.Project{Root: w.repo, WorkRoot: w.repo}}
}

func (w *world) commands() *hooks.Commands {
	return hooks.New(w.sources())
}

func (w *world) hooks(t *testing.T) []model.Hook {
	t.Helper()
	resolved, err := w.commands().Hooks(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func (w *world) only(t *testing.T) model.Hook {
	t.Helper()
	resolved := w.hooks(t)
	if len(resolved) != 1 {
		t.Fatalf("want one hook, got %+v", resolved)
	}
	return resolved[0]
}

func declaredErr(t *testing.T, w *world) error {
	t.Helper()
	_, err := hooks.Declared(w.sources())
	return err
}

func TestDeclaredRefusesACommandOutsideTheHooksFolder(t *testing.T) {
	for _, tc := range []struct{ name, command, want string }{
		{"absolute", "/bin/echo", "is an absolute path"},
		{"parent", "../config.yaml", "leaves the hooks folder"},
		{"PATH lookup", "echo", "never looked up on PATH"},
		{"shell line", `"cat > out.json"`, "holds whitespace"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			w.commit(t, "  - event: entry.written\n    command: "+tc.command+"\n")
			err := declaredErr(t, w)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), filepath.Join(".sdd", "config.yaml")+": hooks[0]") {
				t.Fatalf("the command should be refused naming %q and the declaring file, got %v", tc.want, err)
			}
		})
	}
}

func TestDeclaredRefusesACommittedHookIntoLocal(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "local", "mine.sh"), "true")
	w.commit(t, "  - event: entry.written\n    command: local/mine.sh\n")
	err := declaredErr(t, w)
	if err == nil || !strings.Contains(err.Error(), "config.local.yaml") {
		t.Fatalf("a committed hook into local/ should be refused, got %v", err)
	}

	write(t, filepath.Join(w.repo, ".sdd", "config.yaml"), "repo_id: example.com/org/repo\n")
	write(t, filepath.Join(w.repo, ".sdd", "config.local.yaml"), "hooks:\n  - event: entry.written\n    command: local/mine.sh\n")
	err = declaredErr(t, w)
	if err != nil {
		t.Fatalf("a machine-local hook may run a script from local/, got %v", err)
	}
}

func TestDeclaredRefusesACommandFileThatCannotRun(t *testing.T) {
	w := newWorld(t)
	write(t, filepath.Join(w.hooksDir(), "lint.sh"), "#!/bin/sh\n")
	w.commit(t, "  - event: entry.written\n    command: lint.sh\n")
	err := declaredErr(t, w)
	if err == nil || !strings.Contains(err.Error(), "is not executable") {
		t.Fatalf("a command file without execute permission should be refused, got %v", err)
	}
}

func TestDeclaredRefusesACommandThatIsAFolder(t *testing.T) {
	w := newWorld(t)
	err := os.MkdirAll(filepath.Join(w.hooksDir(), "lint"), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	w.commit(t, "  - event: entry.written\n    command: lint\n")
	err = declaredErr(t, w)
	if err == nil || !strings.Contains(err.Error(), "is not a regular file") {
		t.Fatalf("a folder as command should be refused, got %v", err)
	}
}

// Hooks of every layer resolve in order: user-global, work repository,
// project repository, committed before local, each against its own hooks
// folder.
func TestDeclaredReadsEveryLayerOfAProjectDirLayout(t *testing.T) {
	base := t.TempDir()
	global := filepath.Join(base, "config", "sdd", "config.yaml")
	work, project := filepath.Join(base, "work"), filepath.Join(base, "project")
	hook := func(command string) string { return "hooks:\n  - event: entry.written\n    command: " + command + "\n" }
	write(t, global, hook("global.sh"))
	write(t, filepath.Join(work, ".sdd", "config.yaml"), "project_dir: ../project\n"+hook("work.sh"))
	write(t, filepath.Join(work, ".sdd", "config.local.yaml"), hook("local/work-local.sh"))
	write(t, filepath.Join(project, ".sdd", "config.yaml"), "repo_id: example.com/org/project\n"+hook("project.sh"))
	write(t, filepath.Join(project, ".sdd", "config.local.yaml"), hook("local/project-local.sh"))
	script(t, filepath.Join(base, "config", "sdd", "hooks", "global.sh"), "true")
	for _, file := range []string{"work.sh", "local/work-local.sh"} {
		script(t, filepath.Join(work, ".sdd", "hooks", file), "true")
	}
	for _, file := range []string{"project.sh", "local/project-local.sh"} {
		script(t, filepath.Join(project, ".sdd", "hooks", file), "true")
	}
	resolved, err := meta.ResolveProject(work)
	if err != nil {
		t.Fatal(err)
	}

	declared, err := hooks.Declared(hooks.Sources{GlobalConfig: global, Project: resolved})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, hook := range declared {
		got = append(got, hook.Origin.Label()+"="+hook.Command+"@"+hook.Origin.Dir)
	}
	want := []string{
		"global=global.sh@" + filepath.Join(base, "config", "sdd", "hooks"),
		"work committed=work.sh@" + filepath.Join(work, ".sdd", "hooks"),
		"work local=local/work-local.sh@" + filepath.Join(work, ".sdd", "hooks"),
		"project committed=project.sh@" + filepath.Join(project, ".sdd", "hooks"),
		"project local=local/project-local.sh@" + filepath.Join(project, ".sdd", "hooks"),
	}
	if !slices.Equal(got, want) {
		t.Fatalf("declared hooks\n %v\nwant\n %v", got, want)
	}
}

func TestHooksReadsTheConfigOnEveryCall(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "lint.sh"), "true")
	script(t, filepath.Join(w.hooksDir(), "notify.sh"), "true")
	commands := w.commands()
	w.commit(t, "  - event: entry.written\n    command: lint.sh\n")
	first, err := commands.Hooks(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	w.commit(t, "  - event: entry.written\n    command: notify.sh\n")
	second, err := commands.Hooks(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].Command != "lint.sh" || len(second) != 1 || second[0].Command != "notify.sh" {
		t.Fatalf("each call should read the config as it is then, got %+v then %+v", first, second)
	}
}

func TestRunRunsTheFileWithoutAShellInItsHooksFolder(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "my lint.sh"), `pwd -P; printf '%s|' "$@"; cat`)
	w.commit(t, "  - event: entry.written\n    command: my\\ lint.sh\n    args: [\"$HOME\", \"a b\"]\n")
	output, err := w.commands().Run(t.Context(), w.only(t), []byte(`{"event":"entry.written"}`))
	if err != nil {
		t.Fatal(err)
	}
	dir, err := filepath.EvalSymlinks(w.hooksDir())
	if err != nil {
		t.Fatal(err)
	}
	want := dir + "\n$HOME|a b|{\"event\":\"entry.written\"}"
	if string(output) != want {
		t.Errorf("the hook should run in its hooks folder with its args unexpanded and the payload on stdin\n got %q\nwant %q", output, want)
	}
}

func TestRunFailsOnANonzeroExitWithItsStderr(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "lint.sh"), "echo lint broke >&2; exit 3")
	w.commit(t, "  - event: entry.written\n    command: lint.sh\n")
	_, err := w.commands().Run(t.Context(), w.only(t), nil)
	if err == nil || !strings.Contains(err.Error(), "exit status 3") || !strings.Contains(err.Error(), "lint broke") {
		t.Fatalf("a nonzero exit should fail naming the status and the hook's stderr, got %v", err)
	}
}

func TestRunStopsTheHookAndItsChildrenWhenTheContextEnds(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "hang.sh"), "sleep 30 & echo $! > child.pid; wait")
	w.commit(t, "  - event: entry.written\n    command: hang.sh\n")
	hook := w.only(t)
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := w.commands().Run(ctx, hook, nil)
	if err == nil {
		t.Fatal("a hook outliving its context should fail")
	}
	elapsed := time.Since(started)
	if elapsed > 5*time.Second {
		t.Errorf("the stopped hook should return promptly, took %s", elapsed)
	}
	raw, err := os.ReadFile(filepath.Join(w.hooksDir(), "child.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatal("the hook's child should be stopped with it, still runs")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
