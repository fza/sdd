package hooks_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/hooks"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

// world is a user-global config folder and one git repository, each with a
// hooks folder, and a state root for the allow record.
type world struct {
	global    string
	repo      string
	stateRoot string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	base := t.TempDir()
	w := &world{global: filepath.Join(base, "config", "sdd", "config.yaml"), repo: filepath.Join(base, "repo"), stateRoot: filepath.Join(base, "state", "sdd")}
	write(t, w.global, "")
	write(t, filepath.Join(w.repo, ".sdd", "config.yaml"), "repo_id: example.com/org/repo\n")
	gitIn(t, w.repo, "init", "-q")
	return w
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// track adds paths, relative to the repository, to its index as a clone
// would hold them.
func (w *world) track(t *testing.T, paths ...string) {
	t.Helper()
	gitIn(t, w.repo, append([]string{"add", "-f", "--"}, paths...)...)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func script(t *testing.T, path, body string) {
	t.Helper()
	write(t, path, "#!/bin/sh\n"+body+"\n")
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func (w *world) hooksDir() string { return filepath.Join(w.repo, ".sdd", "hooks") }

func (w *world) commit(t *testing.T, hooksYAML string) {
	t.Helper()
	write(t, filepath.Join(w.repo, ".sdd", "config.yaml"), "repo_id: example.com/org/repo\nhooks:\n"+hooksYAML)
}

func (w *world) local(t *testing.T, hooksYAML string) {
	t.Helper()
	write(t, filepath.Join(w.repo, ".sdd", "config.local.yaml"), "hooks:\n"+hooksYAML)
}

func (w *world) sources() hooks.Sources {
	return hooks.Sources{GlobalConfig: w.global, Project: meta.Project{Root: w.repo, WorkRoot: w.repo}, Tracker: git.TrackedFiles{}}
}

func (w *world) commands() *hooks.Commands {
	return hooks.New(w.sources(), hooks.NewAllows(w.stateRoot))
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

// allow allows every repository waiting for it as the person would after
// reviewing it.
func (w *world) allow(t *testing.T) {
	t.Helper()
	reviews, err := w.commands().Reviews(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := w.commands().Allow(t.Context(), reviews); err != nil {
		t.Fatal(err)
	}
}

func (w *world) hooksErr(t *testing.T) error {
	t.Helper()
	_, err := w.commands().Hooks(t.Context())
	return err
}

func TestHooksRefuseACommandOutsideTheHooksFolder(t *testing.T) {
	for _, tc := range []struct{ name, command, want string }{
		{"absolute", "/bin/echo", "is an absolute path"},
		{"parent", "../config.yaml", "leaves the hooks folder"},
		{"PATH lookup", "echo", "never looked up on PATH"},
		{"shell line", `"cat > out.json"`, "holds whitespace"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			w.commit(t, "  - event: entry.written\n    command: "+tc.command+"\n")
			err := w.hooksErr(t)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), filepath.Join(".sdd", "config.yaml")+": hooks[0]") {
				t.Fatalf("the command should be refused naming %q and the declaring file, got %v", tc.want, err)
			}
		})
	}
}

func TestHooksRefuseACommittedHookIntoLocal(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "local", "mine.sh"), "true")
	w.commit(t, "  - event: entry.written\n    command: local/mine.sh\n")
	if err := w.hooksErr(t); err == nil || !strings.Contains(err.Error(), "config.local.yaml") {
		t.Fatalf("a committed hook into local/ should be refused, got %v", err)
	}

	w.commit(t, "")
	w.local(t, "  - event: entry.written\n    command: local/mine.sh\n")
	if err := w.hooksErr(t); err != nil {
		t.Fatalf("a machine-local hook may run a script from local/, got %v", err)
	}
}

// A machine-local hook runs only a script of its own in local/, judged by
// the file it reaches, so a committed script a pull may change never runs
// under it, named directly or through a link.
func TestHooksRefuseAMachineLocalHookOutsideLocal(t *testing.T) {
	for _, tc := range []struct {
		name    string
		command string
		setup   func(t *testing.T, w *world)
	}{
		{"committed script", "shared.sh", func(t *testing.T, w *world) {}},
		{"link out of local", "local/link.sh", func(t *testing.T, w *world) {
			if err := os.MkdirAll(filepath.Join(w.hooksDir(), "local"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("../shared.sh", filepath.Join(w.hooksDir(), "local", "link.sh")); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			script(t, filepath.Join(w.hooksDir(), "shared.sh"), "true")
			tc.setup(t, w)
			w.local(t, "  - event: entry.written\n    command: "+tc.command+"\n")
			if err := w.hooksErr(t); err == nil || !strings.Contains(err.Error(), "reaches a script outside local/") {
				t.Fatalf("a machine-local hook outside local/ should be refused, got %v", err)
			}
		})
	}
}

// On a case-insensitive file system another spelling of local/ reaches the
// same folder, and a committed hook may not run a script there.
func TestHooksRefuseACommittedHookIntoLocalSpelledOtherwise(t *testing.T) {
	w := newWorld(t)
	requireCaseInsensitive(t, w.repo)
	script(t, filepath.Join(w.hooksDir(), "local", "mine.sh"), "true")
	w.commit(t, "  - event: entry.written\n    command: Local/mine.sh\n")
	if err := w.hooksErr(t); err == nil || !strings.Contains(err.Error(), "reaches a script in local/") {
		t.Fatalf("a committed hook into Local/ should be refused, got %v", err)
	}
}

func TestHooksRefuseACommandFileThatCannotRun(t *testing.T) {
	w := newWorld(t)
	write(t, filepath.Join(w.hooksDir(), "lint.sh"), "#!/bin/sh\n")
	w.commit(t, "  - event: entry.written\n    command: lint.sh\n")
	if err := w.hooksErr(t); err == nil || !strings.Contains(err.Error(), "is not executable") {
		t.Fatalf("a command file without execute permission should be refused, got %v", err)
	}
}

func TestHooksRefuseACommandThatIsAFolder(t *testing.T) {
	w := newWorld(t)
	if err := os.MkdirAll(filepath.Join(w.hooksDir(), "lint"), 0o755); err != nil {
		t.Fatal(err)
	}
	w.commit(t, "  - event: entry.written\n    command: lint\n")
	if err := w.hooksErr(t); err == nil || !strings.Contains(err.Error(), "is not a regular file") {
		t.Fatalf("a folder as command should be refused, got %v", err)
	}
}

// Hooks of every layer resolve in order: user-global, work repository,
// project repository, committed before local, each against its own hooks
// folder and keyed by its repository's real root.
func TestHooksReadEveryLayerOfAProjectDirLayout(t *testing.T) {
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
	gitIn(t, work, "init", "-q")
	gitIn(t, project, "init", "-q")
	resolved, err := meta.ResolveProject(work, nil)
	if err != nil {
		t.Fatal(err)
	}

	declared, err := hooks.New(hooks.Sources{GlobalConfig: global, Project: resolved, Tracker: git.TrackedFiles{}}, hooks.NewAllows(filepath.Join(base, "state"))).Hooks(t.Context())
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
	realWork, err := filepath.EvalSymlinks(work)
	if err != nil {
		t.Fatal(err)
	}
	if declared[1].Origin.Checkout != realWork || declared[2].Origin.Checkout != realWork || declared[3].Origin.Checkout == realWork {
		t.Errorf("each repository should be keyed by its own real root, got %q, %q and %q", declared[1].Origin.Checkout, declared[2].Origin.Checkout, declared[3].Origin.Checkout)
	}
}

// In a graph_repo layout the clone's machine-local layer is the work
// repository's own file, read once.
func TestHooksReadAGraphRepoLayoutsLocalLayerOnce(t *testing.T) {
	base := t.TempDir()
	global := filepath.Join(base, "config", "sdd", "config.yaml")
	work, clone := filepath.Join(base, "work"), filepath.Join(base, "clone")
	write(t, global, "")
	write(t, filepath.Join(work, ".sdd", "config.yaml"), "graph_repo: example.com/org/graph\n")
	write(t, filepath.Join(work, ".sdd", "config.local.yaml"), "hooks:\n  - event: entry.written\n    command: local/mine.sh\n")
	write(t, filepath.Join(clone, ".sdd", "config.yaml"), "repo_id: example.com/org/graph\n")
	script(t, filepath.Join(work, ".sdd", "hooks", "local", "mine.sh"), "true")

	declared, err := hooks.New(hooks.Sources{GlobalConfig: global, Project: meta.Project{Root: clone, WorkRoot: work, GraphRepo: "example.com/org/graph"}}, hooks.NewAllows(filepath.Join(base, "state"))).Hooks(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(declared) != 1 || declared[0].Origin.Label() != "work local" {
		t.Fatalf("the work repository's machine-local layer once, got %+v", declared)
	}
}

// Every layer of a repository waits for an allow, its machine-local one
// included, whether git tracks it or not; only the user-global layer runs
// without.
func TestOnlyTheUserGlobalHookRunsWithoutAnAllow(t *testing.T) {
	w := newWorld(t)
	write(t, w.global, "hooks:\n  - event: entry.written\n    command: notify.sh\n")
	script(t, filepath.Join(filepath.Dir(w.global), "hooks", "notify.sh"), "true")
	script(t, filepath.Join(w.hooksDir(), "lint.sh"), "true")
	script(t, filepath.Join(w.hooksDir(), "local", "mine.sh"), "true")
	w.commit(t, "  - event: entry.written\n    command: lint.sh\n")
	w.local(t, "  - event: entry.written\n    command: local/mine.sh\n")
	w.track(t, ".sdd/config.yaml", ".sdd/hooks/lint.sh")

	resolved := w.hooks(t)
	if len(resolved) != 3 || resolved[0].State != model.HookAllowed || resolved[1].State != model.HookPending || resolved[2].State != model.HookPending {
		t.Fatalf("the global hook should run and both repository layers wait, got %+v", resolved)
	}
	w.allow(t)
	for _, hook := range w.hooks(t) {
		if hook.State != model.HookAllowed {
			t.Errorf("after the allow %s should be allowed, got %s", hook.Label(), hook.State)
		}
	}
}

// A change to a declaration or to any file git tracks in the hooks folder,
// sourced or named by a flag or not, needs a new allow; a file the
// repository does not track, such as a cache a hook writes, does not.
func TestAChangeToAnythingAPullDeliversNeedsANewAllow(t *testing.T) {
	setup := func(t *testing.T) *world {
		w := newWorld(t)
		script(t, filepath.Join(w.hooksDir(), "vale.sh"), ". ./lib.sh")
		write(t, filepath.Join(w.hooksDir(), "lib.sh"), "true\n")
		write(t, filepath.Join(w.hooksDir(), "vale.ini"), "MinAlertLevel = error\n")
		write(t, filepath.Join(w.hooksDir(), "styles", "Base", "rule.yml"), "level: error\n")
		w.commit(t, "  - event: capture.assemble\n    command: vale.sh\n    args: [--config=vale.ini]\n")
		w.track(t, ".sdd")
		w.allow(t)
		if state := w.only(t).State; state != model.HookAllowed {
			t.Fatalf("an allowed hook should resolve allowed, got %s", state)
		}
		return w
	}
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, w *world)
		want   model.HookState
	}{
		{"command file", func(t *testing.T, w *world) { script(t, filepath.Join(w.hooksDir(), "vale.sh"), "echo changed") }, model.HookChanged},
		{"sourced file", func(t *testing.T, w *world) { write(t, filepath.Join(w.hooksDir(), "lib.sh"), "echo evil\n") }, model.HookChanged},
		{"flag file", func(t *testing.T, w *world) {
			write(t, filepath.Join(w.hooksDir(), "vale.ini"), "MinAlertLevel = suggestion\n")
		}, model.HookChanged},
		{"exec bit", func(t *testing.T, w *world) {
			if err := os.Chmod(filepath.Join(w.hooksDir(), "lib.sh"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, model.HookChanged},
		{"newly tracked file", func(t *testing.T, w *world) {
			write(t, filepath.Join(w.hooksDir(), "styles", "Base", "new.yml"), "level: warning\n")
			w.track(t, ".sdd/hooks/styles/Base/new.yml")
		}, model.HookChanged},
		{"declaration", func(t *testing.T, w *world) {
			w.commit(t, "  - event: capture.assemble\n    command: vale.sh\n    args: [--config=vale.ini]\n    timeout: 30\n")
		}, model.HookChanged},
		{"untracked cache", func(t *testing.T, w *world) {
			write(t, filepath.Join(w.hooksDir(), "__pycache__", "lib.pyc"), "cache\n")
		}, model.HookAllowed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := setup(t)
			tc.change(t, w)
			if state := w.only(t).State; state != tc.want {
				t.Errorf("after changing the %s the hook should be %s, got %s", tc.name, tc.want, state)
			}
		})
	}
}

// A machine-local layer a repository tracks waits for the allow and its
// tracked scripts join the hash, however the repository spells its folders
// and through a tracked link to the .sdd folder.
func TestATrackedMachineLocalLayerRunsOnlyOnceAllowed(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, w *world) (script string)
	}{
		{"plain", func(t *testing.T, w *world) string {
			path := filepath.Join(w.hooksDir(), "local", "evil.sh")
			script(t, path, "echo v1")
			w.local(t, "  - event: entry.written\n    command: local/evil.sh\n")
			w.track(t, ".sdd/config.local.yaml", ".sdd/hooks/local/evil.sh")
			return path
		}},
		{"other case", func(t *testing.T, w *world) string {
			requireCaseInsensitive(t, w.repo)
			path := filepath.Join(w.hooksDir(), "LOCAL", "evil.sh")
			script(t, path, "echo v1")
			write(t, filepath.Join(w.repo, ".sdd", "CONFIG.LOCAL.YAML"), "hooks:\n  - event: entry.written\n    command: local/evil.sh\n")
			w.track(t, ".sdd/CONFIG.LOCAL.YAML", ".sdd/hooks/LOCAL/evil.sh")
			return path
		}},
		{"linked .sdd", func(t *testing.T, w *world) string {
			real := filepath.Join(w.repo, "sddreal")
			if err := os.Rename(filepath.Join(w.repo, ".sdd"), real); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("sddreal", filepath.Join(w.repo, ".sdd")); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(real, "hooks", "local", "evil.sh")
			script(t, path, "echo v1")
			write(t, filepath.Join(real, "config.local.yaml"), "hooks:\n  - event: entry.written\n    command: local/evil.sh\n")
			w.track(t, ".sdd", "sddreal")
			return path
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			path := tc.setup(t, w)
			if state := w.only(t).State; state != model.HookPending {
				t.Fatalf("a tracked machine-local hook should wait for the allow, got %s", state)
			}
			w.allow(t)
			if state := w.only(t).State; state != model.HookAllowed {
				t.Fatalf("once allowed it should run, got %s", state)
			}
			script(t, path, "echo v2")
			if state := w.only(t).State; state != model.HookChanged {
				t.Errorf("a change to its tracked script should need a new allow, got %s", state)
			}
		})
	}
}

func requireCaseInsensitive(t *testing.T, dir string) {
	t.Helper()
	probe := filepath.Join(dir, "case-probe")
	write(t, probe, "")
	_, err := os.Stat(filepath.Join(dir, "CASE-PROBE"))
	if err := os.Remove(probe); err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Skip("the file system tells case apart")
	}
}

// Outside a repository git can list, no repository hook runs or can be
// allowed: there is no saying what came with it.
func TestHooksOutsideAGitRepositoryFailClosed(t *testing.T) {
	w := newWorld(t)
	if err := os.RemoveAll(filepath.Join(w.repo, ".git")); err != nil {
		t.Fatal(err)
	}
	script(t, filepath.Join(w.hooksDir(), "local", "mine.sh"), "true")
	w.local(t, "  - event: entry.written\n    command: local/mine.sh\n")

	hook := w.only(t)
	if hook.State != model.HookPending || !strings.Contains(hook.Notice, "cannot run or be allowed") {
		t.Fatalf("a local hook outside a repository should wait, saying why, got %+v", hook)
	}
	if _, err := w.commands().Reviews(t.Context()); err == nil || !strings.Contains(err.Error(), "listing the files git tracks") {
		t.Fatalf("its review should fail naming git, got %v", err)
	}
}

// A tracked link is hashed by its text and never followed, so a link out of
// the repository to a folder sdd cannot read, or to the whole disk, neither
// breaks nor slows reading the hooks.
func TestALinkOutOfTheRepositoryIsHashedByItsText(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "lint.sh"), "true")
	w.commit(t, "  - event: entry.written\n    command: lint.sh\n")
	locked := filepath.Join(t.TempDir(), "locked")
	if err := os.MkdirAll(filepath.Join(locked, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(locked, "inner"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(locked, "inner"), 0o755) })
	for name, target := range map[string]string{"ext": locked, "root": "/"} {
		if err := os.Symlink(target, filepath.Join(w.hooksDir(), name)); err != nil {
			t.Fatal(err)
		}
	}
	w.track(t, ".sdd")

	started := time.Now()
	w.allow(t)
	if state := w.only(t).State; state != model.HookAllowed {
		t.Fatalf("hooks beside links out of the repository should be allowable, got %s", state)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("reading the hooks should not walk the links, took %s", elapsed)
	}
	reviews, err := hooks.New(w.sources(), hooks.NewAllows(t.TempDir())).Reviews(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, file := range reviews[0].Files {
		files = append(files, file.Path+"="+file.Digest)
	}
	if !slices.Contains(files, ".sdd/hooks/root=link:/") || !slices.Contains(files, ".sdd/hooks/ext=link:"+locked) {
		t.Errorf("the links should be covered by their text, got %v", files)
	}
}

// The allow is keyed by the checkout's real root, which a repository cannot
// choose: another checkout with the same repo_id and the same hooks waits
// for its own allow.
func TestAnAllowHoldsForItsCheckoutAlone(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "lint.sh"), "true")
	w.commit(t, "  - event: entry.written\n    command: lint.sh\n")
	w.track(t, ".sdd")
	w.allow(t)

	other := newWorld(t)
	script(t, filepath.Join(other.hooksDir(), "lint.sh"), "true")
	other.commit(t, "  - event: entry.written\n    command: lint.sh\n")
	other.track(t, ".sdd")
	other.stateRoot = w.stateRoot
	if hook := other.only(t); hook.State != model.HookPending {
		t.Errorf("a checkout claiming the same repo_id should wait for its own allow, got %s", hook.State)
	}
}

func TestTheAllowRecordIsAPrivateFileKeyedByRealRoot(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "lint.sh"), "true")
	w.commit(t, "  - event: entry.written\n    command: lint.sh\n")
	w.allow(t)
	hook := w.only(t)

	allows := hooks.NewAllows(w.stateRoot)
	info, err := os.Stat(allows.Path())
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("the allow record should be private, mode %o", mode)
	}
	dir, err := os.Stat(filepath.Dir(allows.Path()))
	if err != nil {
		t.Fatal(err)
	}
	if mode := dir.Mode().Perm(); mode != 0o700 {
		t.Errorf("the allow record's folder should be private, mode %o", mode)
	}
	if raw, _ := os.ReadFile(allows.Path()); !strings.Contains(string(raw), hook.Origin.Checkout) || !strings.Contains(string(raw), hook.Hash) {
		t.Errorf("the record should map the real root to the hash, got %s", raw)
	}
}

// An unreadable record allows nothing and says how to recover, and the next
// allow rewrites it.
func TestACorruptAllowRecordAllowsNothingUntilTheNextAllow(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "lint.sh"), "true")
	w.commit(t, "  - event: entry.written\n    command: lint.sh\n")
	w.allow(t)
	write(t, hooks.NewAllows(w.stateRoot).Path(), "{")

	hook := w.only(t)
	if hook.State != model.HookPending || !strings.Contains(hook.Notice, "is unreadable") || !strings.Contains(hook.Notice, "`sdd hooks allow` rewrites it") {
		t.Fatalf("an unreadable record should allow nothing and say how to recover, got %+v", hook)
	}
	w.allow(t)
	if state := w.only(t).State; state != model.HookAllowed {
		t.Errorf("the next allow should rewrite the record, got %s", state)
	}
}

// Only what the person saw is allowed: a change between the review and the
// answer refuses the allow.
func TestAllowRefusesHooksChangedSinceTheReview(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "lint.sh"), "true")
	w.commit(t, "  - event: entry.written\n    command: lint.sh\n")
	w.track(t, ".sdd")
	reviews, err := w.commands().Reviews(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	script(t, filepath.Join(w.hooksDir(), "lint.sh"), "echo swapped")
	if err := w.commands().Allow(t.Context(), reviews); err == nil || !strings.Contains(err.Error(), "changed since they were shown") {
		t.Fatalf("an allow of hooks changed since the review should be refused, got %v", err)
	}
	if state := w.only(t).State; state != model.HookPending {
		t.Errorf("nothing should be allowed, got %s", state)
	}
}

func TestReviewShowsTheHooksTheCoveredFilesAndTheHeadOfEachCommand(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "vale.sh"), strings.Repeat("echo line\n", 30))
	write(t, filepath.Join(w.hooksDir(), "vale.ini"), "x\n")
	w.commit(t, "  - event: capture.assemble\n    command: vale.sh\n    args: [vale.ini, --flag]\n")
	w.track(t, ".sdd/hooks")
	reviews, err := w.commands().Reviews(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 1 || len(reviews[0].Hooks) != 1 || reviews[0].State != model.HookPending || len(reviews[0].Hash) != 64 {
		t.Fatalf("one pending review with its hook and hash, got %+v", reviews)
	}
	review := reviews[0]
	var files []string
	for _, file := range review.Files {
		files = append(files, file.Path)
	}
	if !slices.Equal(files, []string{".sdd/hooks/vale.ini", ".sdd/hooks/vale.sh"}) {
		t.Errorf("the review should list the tracked files of the hooks folder, got %v", files)
	}
	head := review.Heads[review.Hooks[0].Path]
	if len(head) != 20 || head[0] != "#!/bin/sh" {
		t.Errorf("the review should show the first 20 lines of the command, got %d starting %q", len(head), head)
	}
}

func TestRunRunsTheFileWithoutAShellInItsHooksFolder(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "my lint.sh"), `pwd -P; printf '%s|' "$@"; cat`)
	w.commit(t, "  - event: entry.written\n    command: my\\ lint.sh\n    args: [\"$HOME\", \"a b\"]\n")
	w.allow(t)
	output, err := w.commands().Run(t.Context(), w.only(t), []byte(`{"event":"entry.written"}`))
	if err != nil {
		t.Fatal(err)
	}
	dir, err := filepath.EvalSymlinks(w.hooksDir())
	if err != nil {
		t.Fatal(err)
	}
	if want := dir + "\n$HOME|a b|{\"event\":\"entry.written\"}"; string(output) != want {
		t.Errorf("the hook should run in its hooks folder with its args unexpanded and the payload on stdin\n got %q\nwant %q", output, want)
	}
}

func TestRunFailsOnANonzeroExitWithItsStderr(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "lint.sh"), "echo lint broke >&2; exit 3")
	w.commit(t, "  - event: entry.written\n    command: lint.sh\n")
	w.allow(t)
	_, err := w.commands().Run(t.Context(), w.only(t), nil)
	if err == nil || !strings.Contains(err.Error(), "exit status 3") || !strings.Contains(err.Error(), "lint broke") {
		t.Fatalf("a nonzero exit should fail naming the status and the hook's stderr, got %v", err)
	}
}

// A hook sees only the allowlisted variables of sdd's environment, so keys
// in the MCP server's environment stay with sdd.
func TestRunPassesOnlyTheAllowlistedEnvironment(t *testing.T) {
	t.Setenv("SDD_TEST_SECRET", "s3cret")
	t.Setenv("LC_ALL", "C.UTF-8")
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "env.sh"), "env")
	w.commit(t, "  - event: entry.written\n    command: env.sh\n")
	w.allow(t)
	output, err := w.commands().Run(t.Context(), w.only(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), "s3cret") || !strings.Contains(string(output), "LC_ALL=C.UTF-8") || !strings.Contains(string(output), "PATH=") {
		t.Errorf("the hook should see PATH and the locale but no other variable, got\n%s", output)
	}
}

// A hook's output is capped: stdout past 1 MiB fails it, stderr past its
// cap is cut from the error.
func TestRunCapsTheHooksOutput(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "loud.sh"), "head -c 2000000 /dev/zero")
	script(t, filepath.Join(w.hooksDir(), "noisy.sh"), "head -c 100000 /dev/zero | tr '\\0' x >&2; exit 1")
	w.commit(t, "  - event: entry.written\n    command: loud.sh\n  - event: entry.written\n    command: noisy.sh\n")
	w.allow(t)
	resolved := w.hooks(t)
	if _, err := w.commands().Run(t.Context(), resolved[0], nil); err == nil || !strings.Contains(err.Error(), "more than 1 MiB to stdout") {
		t.Errorf("stdout past its cap should fail the hook, got %v", err)
	}
	_, err := w.commands().Run(t.Context(), resolved[1], nil)
	if err == nil || !strings.Contains(err.Error(), "(stderr cut)") || len(err.Error()) > 5000 {
		t.Errorf("stderr past its cap should be cut from the error, got %d bytes", len(fmt.Sprint(err)))
	}
}

func TestRunRefusesAHookChangedSinceItResolved(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "lint.sh"), "true")
	w.commit(t, "  - event: entry.written\n    command: lint.sh\n")
	w.track(t, ".sdd")
	w.allow(t)
	hook := w.only(t)
	script(t, filepath.Join(w.hooksDir(), "lint.sh"), "echo swapped")
	if _, err := w.commands().Run(t.Context(), hook, nil); err == nil || !strings.Contains(err.Error(), "is not allowed as it is now") {
		t.Fatalf("a hook whose file changed after it resolved should not run, got %v", err)
	}
}

func TestRunStopsTheHookAndItsChildrenWhenTheContextEnds(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "hang.sh"), "sleep 30 & echo $! > child.pid; wait")
	w.commit(t, "  - event: entry.written\n    command: hang.sh\n")
	w.allow(t)
	hook := w.only(t)
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := w.commands().Run(ctx, hook, nil); err == nil {
		t.Fatal("a hook outliving its context should fail")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
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

func TestSweepDropsTheAllowsOfVanishedCheckouts(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "lint.sh"), "true")
	w.commit(t, "  - event: entry.written\n    command: lint.sh\n")
	w.allow(t)
	allows := hooks.NewAllows(w.stateRoot)
	gone := filepath.Join(t.TempDir(), "removed")
	if err := allows.Record(map[string]string{gone: "hash"}); err != nil {
		t.Fatal(err)
	}
	if err := allows.Sweep(); err != nil {
		t.Fatal(err)
	}
	if state, _ := allows.State(gone, "hash"); state != model.HookPending {
		t.Errorf("the allow of a vanished checkout should be dropped, got %s", state)
	}
	if state := w.only(t).State; state != model.HookAllowed {
		t.Errorf("the allow of a present checkout should stay, got %s", state)
	}
}
