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
// hooks folder, and a state root for confirmations.
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
	return w
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

func (w *world) sources() hooks.Sources {
	return hooks.Sources{GlobalConfig: w.global, Project: meta.Project{Root: w.repo, WorkRoot: w.repo}}
}

func (w *world) commands() *hooks.Commands {
	return hooks.New(w.sources(), hooks.NewStore(w.stateRoot))
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
	if err := declaredErr(t, w); err == nil || !strings.Contains(err.Error(), "config.local.yaml") {
		t.Fatalf("a committed hook into local/ should be refused, got %v", err)
	}

	write(t, filepath.Join(w.repo, ".sdd", "config.yaml"), "repo_id: example.com/org/repo\n")
	write(t, filepath.Join(w.repo, ".sdd", "config.local.yaml"), "hooks:\n  - event: entry.written\n    command: local/mine.sh\n")
	if err := declaredErr(t, w); err != nil {
		t.Fatalf("a machine-local hook may run a script from local/, got %v", err)
	}
}

func TestDeclaredRefusesACommandFileThatCannotRun(t *testing.T) {
	w := newWorld(t)
	write(t, filepath.Join(w.hooksDir(), "lint.sh"), "#!/bin/sh\n")
	w.commit(t, "  - event: entry.written\n    command: lint.sh\n")
	if err := declaredErr(t, w); err == nil || !strings.Contains(err.Error(), "is not executable") {
		t.Fatalf("a command file without execute permission should be refused, got %v", err)
	}
}

// Hooks of every layer resolve in order: user-global, work repository,
// project repository, committed before local, each against its own hooks
// folder (20261109-213250-d-tac-bve).
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
	resolved, err := meta.ResolveProject(work, nil)
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
	if declared[3].Origin.RepoKey != "example.com/org/project" || !strings.HasPrefix(declared[1].Origin.RepoKey, "local/") {
		t.Errorf("a repository is keyed by its repo_id, else by its checkout, got %q and %q", declared[3].Origin.RepoKey, declared[1].Origin.RepoKey)
	}
}

func TestOnlyTheUserGlobalHookRunsUnconfirmed(t *testing.T) {
	w := newWorld(t)
	write(t, w.global, "hooks:\n  - event: entry.written\n    command: notify.sh\n")
	script(t, filepath.Join(filepath.Dir(w.global), "hooks", "notify.sh"), "true")
	script(t, filepath.Join(w.hooksDir(), "lint.sh"), "true")
	w.commit(t, "  - event: entry.written\n    command: lint.sh\n")

	resolved := w.hooks(t)
	if len(resolved) != 2 || resolved[0].State != model.HookConfirmed || resolved[1].State != model.HookPending {
		t.Fatalf("the global hook should run unconfirmed and the project's wait, got %+v", resolved)
	}
	if len(resolved[1].Hash) != 64 {
		t.Errorf("a hook should carry its SHA-256 hash, got %q", resolved[1].Hash)
	}
}

// Changing the command file, an args file, a files match or the hook's
// config makes a confirmed hook pending again.
func TestAnyChangeMakesAConfirmedHookAskAgain(t *testing.T) {
	setup := func(t *testing.T) *world {
		w := newWorld(t)
		script(t, filepath.Join(w.hooksDir(), "vale.sh"), "true")
		write(t, filepath.Join(w.hooksDir(), "vale.ini"), "MinAlertLevel = error\n")
		write(t, filepath.Join(w.hooksDir(), "styles", "Base", "rule.yml"), "level: error\n")
		w.commit(t, "  - event: capture.assemble\n    command: vale.sh\n    args: [--config, vale.ini]\n    files: [styles]\n")
		if err := w.commands().Confirm(t.Context(), w.only(t)); err != nil {
			t.Fatal(err)
		}
		if state := w.only(t).State; state != model.HookConfirmed {
			t.Fatalf("a confirmed hook should resolve confirmed, got %s", state)
		}
		return w
	}
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, w *world)
		want   model.HookState
	}{
		{"command file", func(t *testing.T, w *world) { script(t, filepath.Join(w.hooksDir(), "vale.sh"), "echo changed") }, model.HookChanged},
		{"args file", func(t *testing.T, w *world) {
			write(t, filepath.Join(w.hooksDir(), "vale.ini"), "MinAlertLevel = suggestion\n")
		}, model.HookChanged},
		{"files match", func(t *testing.T, w *world) {
			write(t, filepath.Join(w.hooksDir(), "styles", "Base", "new.yml"), "level: warning\n")
		}, model.HookChanged},
		{"config", func(t *testing.T, w *world) {
			w.commit(t, "  - event: capture.assemble\n    command: vale.sh\n    args: [--config, vale.ini]\n    files: [styles]\n    timeout: 30\n")
		}, model.HookChanged},
		{"event", func(t *testing.T, w *world) {
			w.commit(t, "  - event: capture.guide\n    command: vale.sh\n    args: [--config, vale.ini]\n    files: [styles]\n")
		}, model.HookPending},
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

func TestAFilesEntryMatchingNothingIsRefused(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "vale.sh"), "true")
	w.commit(t, "  - event: capture.assemble\n    command: vale.sh\n    files: [styles/*.yml]\n")
	if _, err := w.commands().Hooks(t.Context()); err == nil || !strings.Contains(err.Error(), `files entry "styles/*.yml" matches nothing`) {
		t.Fatalf("a files entry matching nothing should be refused, got %v", err)
	}
}

func TestConfirmationsLiveInAPrivateFilePerRepositoryKey(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "lint.sh"), "true")
	w.commit(t, "  - event: entry.written\n    command: lint.sh\n")
	hook := w.only(t)
	if err := w.commands().Confirm(t.Context(), hook); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(w.stateRoot, "hooks", "example.com", "org", "repo.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the confirmation should live at %s: %v", path, err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("the confirmation file should be private, mode %o", mode)
	}
	if raw, _ := os.ReadFile(path); !strings.Contains(string(raw), hook.Hash) || !strings.Contains(string(raw), w.repo) || !strings.Contains(string(raw), `"event": "entry.written"`) {
		t.Errorf("the confirmation should record the hash, event and checkout, got %s", raw)
	}

	clone := newWorld(t)
	script(t, filepath.Join(clone.hooksDir(), "lint.sh"), "true")
	clone.commit(t, "  - event: entry.written\n    command: lint.sh\n")
	clone.stateRoot = w.stateRoot
	if state := clone.only(t).State; state != model.HookConfirmed {
		t.Errorf("a clone of the same repository should share the confirmation, got %s", state)
	}
}

func TestSweepDropsUndeclaredHashesAndVanishedCheckouts(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "lint.sh"), "true")
	w.commit(t, "  - event: entry.written\n    command: lint.sh\n")
	kept := w.only(t)
	store := hooks.NewStore(w.stateRoot)
	if err := store.Confirm(kept); err != nil {
		t.Fatal(err)
	}
	dropped := kept
	dropped.Hash = strings.Repeat("0", 64)
	gone := kept
	gone.Hash, gone.Origin.Checkout = strings.Repeat("1", 64), filepath.Join(t.TempDir(), "removed")
	for _, hook := range []model.Hook{dropped, gone} {
		if err := store.Confirm(hook); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(gone.Origin.Checkout); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	err := store.Sweep(func(checkout string) ([]string, error) {
		if checkout != w.repo {
			t.Errorf("only existing checkouts should be asked, asked %s", checkout)
		}
		return []string{kept.Hash}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(store.Path(kept.Origin.RepoKey))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), kept.Hash) || strings.Contains(string(raw), dropped.Hash) || strings.Contains(string(raw), gone.Hash) {
		t.Errorf("the sweep should keep only the declared hash, store holds %s", raw)
	}
}

func TestSweepKeepsTheConfirmationsOfACheckoutItCannotRead(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "lint.sh"), "true")
	w.commit(t, "  - event: entry.written\n    command: lint.sh\n")
	hook := w.only(t)
	store := hooks.NewStore(w.stateRoot)
	if err := store.Confirm(hook); err != nil {
		t.Fatal(err)
	}
	err := store.Sweep(func(string) ([]string, error) { return nil, os.ErrPermission })
	if err == nil || !strings.Contains(err.Error(), w.repo) {
		t.Fatalf("a checkout the sweep cannot read should be reported, got %v", err)
	}
	if state := w.only(t).State; state != model.HookConfirmed {
		t.Errorf("its confirmation should be kept, got %s", state)
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
	if want := dir + "\n$HOME|a b|{\"event\":\"entry.written\"}"; string(output) != want {
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

func TestRunRefusesAHookChangedSinceItResolved(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "lint.sh"), "true")
	w.commit(t, "  - event: entry.written\n    command: lint.sh\n")
	hook := w.only(t)
	script(t, filepath.Join(w.hooksDir(), "lint.sh"), "echo swapped")
	if _, err := w.commands().Run(t.Context(), hook, nil); err == nil || !strings.Contains(err.Error(), "changed while the call ran") {
		t.Fatalf("a hook whose file changed after it resolved should not run, got %v", err)
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

func TestReviewShowsTheHashedFilesAndTheHeadOfTheCommand(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "vale.sh"), strings.Repeat("echo line\n", 30))
	write(t, filepath.Join(w.hooksDir(), "vale.ini"), "x\n")
	w.commit(t, "  - event: capture.assemble\n    command: vale.sh\n    args: [vale.ini, --flag]\n")
	review, err := w.commands().Review(t.Context(), w.only(t))
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, file := range review.Files {
		files = append(files, file.Role+":"+file.Path)
		if len(file.Digest) != 64 {
			t.Errorf("%s should carry its SHA-256 digest, got %q", file.Path, file.Digest)
		}
	}
	if !slices.Equal(files, []string{"command:vale.sh", "arg:vale.ini"}) {
		t.Errorf("the review should list the command and the args naming a file, got %v", files)
	}
	if len(review.Head) != 20 || review.Head[0] != "#!/bin/sh" {
		t.Errorf("the review should show the first 20 lines of the command, got %d starting %q", len(review.Head), review.Head)
	}
}

func TestSweepAsksTheCheckoutsConfigAsItIsNow(t *testing.T) {
	w := newWorld(t)
	script(t, filepath.Join(w.hooksDir(), "lint.sh"), "true")
	w.commit(t, "  - event: entry.written\n    command: lint.sh\n")
	store := hooks.NewStore(w.stateRoot)
	if err := store.Confirm(w.only(t)); err != nil {
		t.Fatal(err)
	}
	declared := hooks.DeclaredIn(w.global, nil)

	if err := store.Sweep(declared); err != nil {
		t.Fatal(err)
	}
	if state := w.only(t).State; state != model.HookConfirmed {
		t.Fatalf("a hook the checkout still declares should stay confirmed, got %s", state)
	}

	w.commit(t, "")
	if err := store.Sweep(declared); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.Path("example.com/org/repo")); !os.IsNotExist(err) {
		t.Errorf("a store left without confirmations should be removed, stat %v", err)
	}
}

func TestDeclaredRefusesACommandThatIsAFolder(t *testing.T) {
	w := newWorld(t)
	if err := os.MkdirAll(filepath.Join(w.hooksDir(), "lint"), 0o755); err != nil {
		t.Fatal(err)
	}
	w.commit(t, "  - event: entry.written\n    command: lint\n")
	if err := declaredErr(t, w); err == nil || !strings.Contains(err.Error(), "is not a regular file") {
		t.Fatalf("a folder as command should be refused, got %v", err)
	}
}

// In a graph_repo layout the clone's machine-local layer is the work
// repository's own file, read once.
func TestDeclaredReadsAGraphRepoLayoutsLocalLayerOnce(t *testing.T) {
	base := t.TempDir()
	global := filepath.Join(base, "config", "sdd", "config.yaml")
	work, clone := filepath.Join(base, "work"), filepath.Join(base, "clone")
	write(t, global, "")
	write(t, filepath.Join(work, ".sdd", "config.yaml"), "graph_repo: example.com/org/graph\n")
	write(t, filepath.Join(work, ".sdd", "config.local.yaml"), "hooks:\n  - event: entry.written\n    command: local/mine.sh\n")
	write(t, filepath.Join(clone, ".sdd", "config.yaml"), "repo_id: example.com/org/graph\n")
	script(t, filepath.Join(work, ".sdd", "hooks", "local", "mine.sh"), "true")

	declared, err := hooks.Declared(hooks.Sources{GlobalConfig: global, Project: meta.Project{Root: clone, WorkRoot: work, GraphRepo: "example.com/org/graph"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(declared) != 1 || declared[0].Origin.Label() != "work local" {
		t.Fatalf("the work repository's machine-local layer once, got %+v", declared)
	}
}
