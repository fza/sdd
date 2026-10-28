package application_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

type recordingGraphSync struct {
	mu       sync.Mutex
	branches []string
	triggers []sdd.SyncTrigger
	sessions []string
	notice   string
	err      error
	interval time.Duration
	blocked  *sdd.SyncBlocked
}

func (s *recordingGraphSync) SyncGraph(_ context.Context, branch string, trigger sdd.SyncTrigger) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.branches = append(s.branches, branch)
	s.triggers = append(s.triggers, trigger)
	return s.err
}

func (s *recordingGraphSync) SyncNotice(_ context.Context, _, session string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = append(s.sessions, session)
	return s.notice, nil
}

func (s *recordingGraphSync) SyncInterval(context.Context, string) (time.Duration, bool, error) {
	return s.interval, s.interval > 0, nil
}

func (s *recordingGraphSync) rounds() []sdd.SyncTrigger {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.triggers)
}

type branchlessCheckout struct{}

func (branchlessCheckout) BaseBranch(context.Context) (string, error) { return "", nil }

func syncedApplication(t *testing.T, sync *recordingGraphSync, base ...sdd.BaseBranchResolver) (*sdd.Application, *sdd.ProjectRuntime) {
	t.Helper()
	var baseResolver sdd.BaseBranchResolver
	if len(base) > 0 {
		baseResolver = base[0]
	}
	graph, err := localadapter.NewFilesystemGraphStore(localadapter.FilesystemGraphStoreOptions{Project: "example", GraphDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := sdd.NewProjectRuntime(sdd.ProjectRuntimeOptions{
		Project: sdd.ProjectRef{ID: "example"}, DefaultBranch: "main", Graph: graph, Sync: sync, Base: baseResolver,
		LLM: pkgllm.RunnerFunc(func(context.Context, pkgllm.Request) (pkgllm.Result, error) {
			return pkgllm.Result{}, nil
		}),
	})
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
	application, err := sdd.NewApplication(sdd.ApplicationOptions{Access: &runtimeAccessResolver{runtime: runtime}, Sessions: sessions, StagedBlobs: blobs})
	if err != nil {
		t.Fatal(err)
	}
	return application, runtime
}

func TestRefreshWorkflowSyncsTheSessionBranchFirst(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "sync succeeds"},
		{name: "sync fails", err: errors.New("upstream unreachable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sync := &recordingGraphSync{err: tc.err}
			application, _ := syncedApplication(t, sync)
			identity := sdd.RequestIdentity{Subject: "christopher"}
			workflow, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "test"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := application.RefreshWorkflow(t.Context(), identity, workflow.ID()); err != nil {
				t.Fatalf("a sync failure should not block the session: %v", err)
			}
			if !slices.Equal(sync.branches, []string{"main"}) || !slices.Equal(sync.triggers, []sdd.SyncTrigger{sdd.SyncOnCall}) {
				t.Errorf("a refresh of an unbound session should run one call round on the base branch, got %v %v", sync.branches, sync.triggers)
			}
		})
	}
}

func TestSyncSkipsACheckoutWithoutBranch(t *testing.T) {
	sync := &recordingGraphSync{err: errors.New("no registered checkout"), blocked: &sdd.SyncBlocked{Kind: "no-upstream"}}
	application, _ := syncedApplication(t, sync, branchlessCheckout{})
	identity := sdd.RequestIdentity{Subject: "christopher"}
	info, err := application.Info(t.Context(), identity, "example", sdd.InfoRequest{})
	if err != nil {
		t.Fatalf("info should not read sync state of a checkout without branch: %v", err)
	}
	if info.SyncBlocked != nil {
		t.Error("a checkout without branch should report no blocked sync")
	}
	workflow, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.RefreshWorkflow(t.Context(), identity, workflow.ID()); err != nil {
		t.Fatal(err)
	}
	if len(sync.rounds()) != 0 {
		t.Error("a checkout without branch should run no sync round")
	}
}

func TestSyncNoticeNamesTheSession(t *testing.T) {
	sync := &recordingGraphSync{notice: "sync blocked since 2026-10-28 12:00: the branch has no upstream"}
	application, _ := syncedApplication(t, sync)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	workflow, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if got := workflow.SyncNotice(t.Context(), identity); got != sync.notice {
		t.Errorf("the notice of the session's branch should be served, got %q", got)
	}
	if !slices.Equal(sync.sessions, []string{string(workflow.ID())}) {
		t.Errorf("the notice should be claimed for the calling session, got %v", sync.sessions)
	}
}

func TestRunSyncTimer(t *testing.T) {
	t.Run("no interval", func(t *testing.T) {
		_, runtime := syncedApplication(t, &recordingGraphSync{})
		if err := runtime.RunSyncTimer(t.Context()); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("ticking until stopped", func(t *testing.T) {
		sync := &recordingGraphSync{interval: 5 * time.Millisecond}
		_, runtime := syncedApplication(t, sync)
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error)
		go func() { done <- runtime.RunSyncTimer(ctx) }()
		deadline := time.After(5 * time.Second)
		for len(sync.rounds()) < 2 {
			select {
			case <-deadline:
				t.Fatal("the timer should run rounds once per interval")
			case <-time.After(time.Millisecond):
			}
		}
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("stopping the server should stop its timer")
		}
		for _, trigger := range sync.rounds() {
			if trigger != sdd.SyncOnTimer {
				t.Errorf("a timer round should always fetch, got trigger %q", trigger)
			}
		}
		stopped := len(sync.rounds())
		time.Sleep(20 * time.Millisecond)
		if len(sync.rounds()) != stopped {
			t.Error("no round should run after the timer stopped")
		}
	})
}

func (s *recordingGraphSync) SyncBlocked(context.Context, string) (*sdd.SyncBlocked, error) {
	return s.blocked, nil
}
