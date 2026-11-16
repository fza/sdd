package hooks_test

import (
	"context"
	"fmt"
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
// hooks folder, and a state root for the allow record.
type world struct {
	global    string
	repo      string
	stateRoot string
	tracked   trackedFiles
}

// trackedFiles is a Tracker reporting its files as tracked wherever they
// fall under the asked paths.
type trackedFiles []string

func (f trackedFiles) Tracked(_ context.Context, _ string, paths ...string) ([]string, error) {
	var tracked []string
	for _, file := range f {
		for _, path := range paths {
			if file == path || strings.HasPrefix(file, path+"/") {
				tracked = append(tracked, file)
			}
		}
	}
	return tracked, nil
}

func newWorld(t *testing.T) *world {
	t.Helper()
	base := t.TempDir()
	w := &world{global: filepath.Join(base, "config", "sdd", "config.yaml"), repo: filepath.Join(base, "repo"), stateRoot: filepath.Join(base, "state", "sdd")}
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
	return hooks.Sources{GlobalConfig: w.global, Project: meta.Project{Root: w.repo, WorkRoot: w.repo}, Tracker: w.tracked}
}

func (w *world) commands() *hooks.Commands {
	return hooks.New(w.sources(), hooks.NewAllows(w.stateRoot))
}

func (w *world) allow(t *testing.T) []model.Hook {
	t.Helper()
	allowed, err := hooks.Allow(t.Context(), w.sources(), hooks.NewAllows(w.stateRoot))
	if err != nil {
		t.Fatal(err)
	}
	return allowed
}

// needsAllow lists, per hook, its command and whether it waits for an allow.
func (w *world) needsAllow(t *testing.T) []string {
	t.Helper()
	var states []string
	for _, hook := range w.hooks(t) {
		states = append(states, fmt.Sprintf("%s=%v", hook.Command, hook.NeedsAllow))
	}
	return states
}

// Only the hooks a repository brings with its committed config wait for an
// allow; the user-global and machine-local ones are the person's own.
func TestCommittedHooksRunOnlyOnceAllowed(t *testing.T) {
	w := newWorld(t)
	write(t, w.global, "hooks:\n  - event: entry.written\n    command: notify.sh\n")
	script(t, filepath.Join(filepath.Dir(w.global), "hooks", "notify.sh"), "true")
	script(t, filepath.Join(w.hooksDir(), "lint.sh"), "true")
	script(t, filepath.Join(w.hooksDir(), "local", "mine.sh"), "true")
	w.commit(t, "  - event: entry.written\n    command: lint.sh\n")
	write(t, filepath.Join(w.repo, ".sdd", "config.local.yaml"), "hooks:\n  - event: entry.written\n    command: local/mine.sh\n")

	want := []string{"notify.sh=false", "lint.sh=true", "local/mine.sh=false"}
	if got := w.needsAllow(t); !slices.Equal(got, want) {
		t.Fatalf("only the committed hook should wait for an allow, got %v", got)
	}
	allowed := w.allow(t)
	if len(allowed) != 1 || allowed[0].Command != "lint.sh" {
		t.Errorf("allow should name the committed hook it allowed, got %+v", allowed)
	}
	want = []string{"notify.sh=false", "lint.sh=false", "local/mine.sh=false"}
	if got := w.needsAllow(t); !slices.Equal(got, want) {
		t.Fatalf("an allowed committed hook should run, got %v", got)
	}

	info, err := os.Stat(filepath.Join(w.stateRoot, "hooks", "allowed.json"))
	if err != nil {
		t.Fatalf("the allow should be recorded in the state root: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("the allow record should be private, mode %o", info.Mode().Perm())
	}
}

// A machine-local layer the repository tracks, its config file or a script
// under hooks/local/, came with the clone: it waits for an allow too.
func TestATrackedLocalLayerWaitsForAnAllow(t *testing.T) {
	for _, tracked := range []string{".sdd/config.local.yaml", ".sdd/hooks/local/mine.sh"} {
		t.Run(tracked, func(t *testing.T) {
			w := newWorld(t)
			w.tracked = trackedFiles{tracked}
			script(t, filepath.Join(w.hooksDir(), "local", "mine.sh"), "true")
			write(t, filepath.Join(w.repo, ".sdd", "config.local.yaml"), "hooks:\n  - event: entry.written\n    command: local/mine.sh\n")
			if !w.only(t).NeedsAllow {
				t.Fatal("a tracked local layer's hook should wait for an allow")
			}
			allowed := w.allow(t)
			if len(allowed) != 1 || allowed[0].Command != "local/mine.sh" {
				t.Errorf("allow should name the tracked local hook, got %+v", allowed)
			}
			if w.only(t).NeedsAllow {
				t.Error("an allowed tracked local hook should run")
			}
		})
	}
}

// Any change to a committed hook's config, an args file or any file in the
// hooks folder outside local/ needs a new allow.
func TestAChangedCommittedHookNeedsAllowAgain(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, w *world)
	}{
		{"script", func(t *testing.T, w *world) { script(t, filepath.Join(w.hooksDir(), "vale.sh"), "echo changed") }},
		{"sourced file", func(t *testing.T, w *world) { write(t, filepath.Join(w.hooksDir(), "lib.sh"), "echo changed\n") }},
		{"file a flag names", func(t *testing.T, w *world) { write(t, filepath.Join(w.hooksDir(), "rules", "data.txt"), "changed\n") }},
		{"new file in the folder", func(t *testing.T, w *world) {
			write(t, filepath.Join(w.hooksDir(), "styles", "x.yml"), "level: error\n")
		}},
		{"args file", func(t *testing.T, w *world) {
			write(t, filepath.Join(w.hooksDir(), "vale.ini"), "MinAlertLevel = suggestion\n")
		}},
		{"config", func(t *testing.T, w *world) {
			w.commit(t, "  - event: capture.assemble\n    command: vale.sh\n    args: [--config, vale.ini]\n    timeout: 30\n")
		}},
		{"added hook", func(t *testing.T, w *world) {
			w.commit(t, "  - event: capture.assemble\n    command: vale.sh\n    args: [--config, vale.ini]\n  - event: entry.written\n    command: vale.sh\n")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			script(t, filepath.Join(w.hooksDir(), "vale.sh"), ". ./lib.sh")
			write(t, filepath.Join(w.hooksDir(), "lib.sh"), "true\n")
			write(t, filepath.Join(w.hooksDir(), "rules", "data.txt"), "rules\n")
			write(t, filepath.Join(w.hooksDir(), "vale.ini"), "MinAlertLevel = error\n")
			w.commit(t, "  - event: capture.assemble\n    command: vale.sh\n    args: [--config, vale.ini, --rules=rules/data.txt]\n")
			w.allow(t)
			if w.only(t).NeedsAllow {
				t.Fatal("an allowed hook should run")
			}
			tc.change(t, w)
			for _, hook := range w.hooks(t) {
				if !hook.NeedsAllow {
					t.Errorf("after a change to the %s the committed hooks should wait for an allow again, %s does not", tc.name, hook.Command)
				}
			}
		})
	}
}

// The machine-local scripts under an untracked local/ are the person's own,
// so changing them leaves the committed hooks allowed.
func TestAChangeUnderAnUntrackedLocalFolderKeepsTheAllow(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "vale.sh"), "true")
	w.commit(t, "  - event: capture.assemble\n    command: vale.sh\n")
	w.allow(t)
	script(t, filepath.Join(w.hooksDir(), "local", "mine.sh"), "echo mine")
	if w.only(t).NeedsAllow {
		t.Error("a change under an untracked local/ should keep the committed hooks allowed")
	}
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

// declaredErr returns the error the one declared hook carries; Declared
// itself succeeds, so a bad hook fails only the calls that reach it.
func declaredErr(t *testing.T, w *world) error {
	t.Helper()
	declared, err := hooks.Declared(t.Context(), w.sources())
	if err != nil {
		t.Fatalf("a bad hook should not fail reading the others: %v", err)
	}
	if len(declared) != 1 {
		t.Fatalf("want the one declared hook, got %+v", declared)
	}
	return declared[0].Err
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
	// ResolveProject returns real paths, so the expected folders are real too.
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
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

	declared, err := hooks.Declared(t.Context(), hooks.Sources{GlobalConfig: global, Project: resolved, Tracker: trackedFiles{}})
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

func TestRunPassesOnlyTheAllowlistedEnvironment(t *testing.T) {
	t.Setenv("FAKE_SECRET", "s3cr3t-token")
	t.Setenv("LANG", "en_US.UTF-8")
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "env.sh"), "env")
	w.commit(t, "  - event: entry.written\n    command: env.sh\n")
	output, err := w.commands().Run(t.Context(), w.only(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), "s3cr3t-token") {
		t.Errorf("a variable of sdd's environment outside the allowlist should not reach the hook:\n%s", output)
	}
	for _, name := range []string{"PATH=", "HOME=", "LANG="} {
		if !strings.Contains(string(output), name) {
			t.Errorf("the hook should see %s from sdd's environment:\n%s", name, output)
		}
	}
}

func TestRunFailsAHookWritingMoreThanTheOutputCap(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "flood.sh"), "head -c 1100000 /dev/zero")
	w.commit(t, "  - event: entry.written\n    command: flood.sh\n")
	_, err := w.commands().Run(t.Context(), w.only(t), nil)
	if err == nil || !strings.Contains(err.Error(), "more than 1 MiB to stdout") {
		t.Fatalf("a hook flooding stdout should fail naming the cap, got %v", err)
	}
}

func TestRunCutsALongStderrInItsError(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "noisy.sh"), "head -c 100000 /dev/zero | tr '\\0' x >&2; exit 1")
	w.commit(t, "  - event: entry.written\n    command: noisy.sh\n")
	_, err := w.commands().Run(t.Context(), w.only(t), nil)
	if err == nil || len(err.Error()) > 10000 || !strings.Contains(err.Error(), "stderr cut") {
		t.Fatalf("a long stderr should be cut in the error, got %d bytes", len(err.Error()))
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
