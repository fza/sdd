package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/application/types"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

// recordingHooks is a HookCommands answering every run with output and
// recording the events of the runs.
type recordingHooks struct {
	mu     sync.Mutex
	hooks  []sdd.Hook
	output string
	events []string
}

func (r *recordingHooks) Hooks(context.Context) ([]sdd.Hook, error) {
	return append([]sdd.Hook(nil), r.hooks...), nil
}

func (r *recordingHooks) Run(_ context.Context, _ sdd.Hook, stdin []byte) ([]byte, error) {
	var payload map[string]any
	err := json.Unmarshal(stdin, &payload)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	event, _ := payload["event"].(string)
	r.events = append(r.events, event)
	return []byte(r.output), nil
}

func (r *recordingHooks) ran() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

func hookAt(event string) sdd.Hook {
	return sdd.Hook{HookConfig: types.HookConfig{Event: event, Command: "hook.sh"}, Origin: types.HookOrigin{Layer: types.HookLayerGlobal, ConfigPath: "/config/sdd/config.yaml"}}
}

// newHookedWorkflow opens a workflow session on the waiting-test procedure's
// graph with hooks configured.
func newHookedWorkflow(t *testing.T, hooks sdd.HookCommands) *sdd.WorkflowSession {
	t.Helper()
	w, _ := openHookedWorkflow(t, hooks)
	return w
}

// openHookedWorkflow is newHookedWorkflow returning the opening serve too.
func openHookedWorkflow(t *testing.T, hooks sdd.HookCommands) (*sdd.WorkflowSession, *sdd.WorkflowServe) {
	t.Helper()
	graphDir := t.TempDir()
	path := filepath.Join(graphDir, "2026", "07", "13-060001-d-prc-wai.md")
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(path, []byte(waitingProcedure), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := localadapter.NewFilesystemGraphStore(localadapter.FilesystemGraphStoreOptions{Project: "example", GraphDir: graphDir})
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := localadapter.NewFilesystemSessionStoreAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := localadapter.NewFilesystemStagedBlobStoreAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := sdd.NewProjectRuntime(sdd.ProjectRuntimeOptions{
		Project: sdd.ProjectRef{ID: "example"}, DefaultBranch: "main", Graph: graph, Hooks: hooks,
		LLM: pkgllm.RunnerFunc(func(context.Context, pkgllm.Request) (pkgllm.Result, error) {
			return pkgllm.Result{Identity: pkgllm.Identity{Provider: "test", Model: "test"}}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	application, err := sdd.NewApplication(sdd.ApplicationOptions{Access: &runtimeAccessResolver{runtime: runtime}, Sessions: sessions, StagedBlobs: blobs})
	if err != nil {
		t.Fatal(err)
	}
	w, opened, err := application.OpenWorkflow(t.Context(), sdd.RequestIdentity{Subject: "christopher"}, "example", sdd.WorkflowOpenRequest{ClientName: "hooks"})
	if err != nil {
		t.Fatal(err)
	}
	return w, opened
}

func startWaiting(t *testing.T, w *sdd.WorkflowSession) *sdd.WorkflowServe {
	t.Helper()
	serve, err := w.Start(t.Context(), sdd.RequestIdentity{Subject: "christopher"}, sdd.WorkflowStartRequest{Canonical: "waiting-test"})
	if err != nil {
		t.Fatal(err)
	}
	return serve
}

func advanceWaiting(w *sdd.WorkflowSession, instance string, report map[string]any) (*sdd.WorkflowServe, error) {
	return w.Advance(context.Background(), sdd.RequestIdentity{Subject: "christopher"}, sdd.WorkflowAdvanceRequest{Instance: instance, Report: report})
}

func TestHookRunsAtTheEventsItMatches(t *testing.T) {
	hooks := &recordingHooks{hooks: []sdd.Hook{hookAt("waiting-test.*.start"), hookAt("waiting-test.collect-both")}}
	w := newHookedWorkflow(t, hooks)
	serve := startWaiting(t, w)
	_, err := advanceWaiting(w, serve.Instance, map[string]any{"body": "first", "anchor": "second"})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(hooks.ran(), ",")
	if got != "waiting-test.collect-both.start,waiting-test.collect-both" {
		t.Errorf("the hooks should run at arrival and when the step is done, ran at %s", got)
	}
}

func TestHookAnsweringWithUndeclaredFindingsFailsTheCall(t *testing.T) {
	hooks := &recordingHooks{hooks: []sdd.Hook{hookAt("waiting-test.collect-both.start")}, output: `{"findings": [{"severity": "low", "category": "c", "observation": "o"}]}`}
	w := newHookedWorkflow(t, hooks)
	_, err := w.Start(t.Context(), sdd.RequestIdentity{Subject: "christopher"}, sdd.WorkflowStartRequest{Canonical: "waiting-test"})
	if err == nil || !strings.Contains(err.Error(), "does not declare findings") {
		t.Fatalf("findings from a hook not declaring them should fail the call, got %v", err)
	}
}

func TestHookWithAProblemFailsOnlyTheCallsReachingIt(t *testing.T) {
	broken := hookAt("waiting-test.collect-both")
	broken.Err = errors.New(`command "hook.sh" is not executable`)
	hooks := &recordingHooks{hooks: []sdd.Hook{broken}}
	w := newHookedWorkflow(t, hooks)
	serve := startWaiting(t, w)
	_, err := advanceWaiting(w, serve.Instance, map[string]any{"body": "first", "anchor": "second"})
	if err == nil || !strings.Contains(err.Error(), "is not executable") {
		t.Fatalf("the call raising the broken hook's event should fail naming its problem, got %v", err)
	}
}

func TestHookWaitingForAnAllowIsSkippedWithANotice(t *testing.T) {
	waiting := hookAt("waiting-test.*.start")
	waiting.NeedsAllow = true
	hooks := &recordingHooks{hooks: []sdd.Hook{waiting}}
	w := newHookedWorkflow(t, hooks)
	serve := startWaiting(t, w)
	if ran := hooks.ran(); len(ran) != 0 {
		t.Fatalf("a hook waiting for an allow should not run, ran at %v", ran)
	}
	if len(serve.Notices) != 1 || !strings.Contains(serve.Notices[0], "sdd hooks allow") {
		t.Fatalf("the serve should say the hook was skipped and name `sdd hooks allow`, got %q", serve.Notices)
	}
	serve = startWaiting(t, w)
	if len(serve.Notices) != 0 {
		t.Errorf("the skip should be noticed once per session, noticed again %q", serve.Notices)
	}
}

func TestHookWaitingForAnAllowCarriesItsOwnNotice(t *testing.T) {
	waiting := hookAt("waiting-test.*.start")
	waiting.NeedsAllow = true
	waiting.Notice = "The hook allow record is unreadable."
	w := newHookedWorkflow(t, &recordingHooks{hooks: []sdd.Hook{waiting}})
	serve := startWaiting(t, w)
	if len(serve.Notices) != 1 || !strings.Contains(serve.Notices[0], waiting.Notice) {
		t.Fatalf("the skip notice should carry the hook's own notice, got %q", serve.Notices)
	}
}

func TestHookNamingAnUnknownEventIsServedOnceAsANotice(t *testing.T) {
	hooks := &recordingHooks{hooks: []sdd.Hook{hookAt("waiting-test.renamed")}}
	w, opened := openHookedWorkflow(t, hooks)
	serve := startWaiting(t, w)
	if len(serve.Notices) == 0 {
		serve = opened
	}
	if len(serve.Notices) != 1 || !strings.Contains(serve.Notices[0], `unknown event "waiting-test.renamed"`) || !strings.Contains(serve.Instructions, "Notice:") {
		t.Fatalf("the first serve after the hooks are read should carry a notice naming the unknown event, got %q", serve.Notices)
	}
	serve, err := advanceWaiting(w, startWaiting(t, w).Instance, map[string]any{"body": "first"})
	if err != nil {
		t.Fatal(err)
	}
	if len(serve.Notices) != 0 {
		t.Errorf("a notice should be served once, served again %q", serve.Notices)
	}
}
