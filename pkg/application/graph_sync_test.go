package application_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
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
	released []string
	notice   string
	err      error
	due      bool
	dueAsked int
	blocked  *sdd.SyncBlocked
	// backfills records each title backfill's trigger; hold, when set, keeps
	// every backfill running until it is closed.
	backfills []sdd.SyncTrigger
	hold      chan struct{}
}

func (s *recordingGraphSync) BackfillTitles(_ context.Context, _ string, trigger sdd.SyncTrigger, runner pkgllm.Runner) error {
	s.mu.Lock()
	s.backfills = append(s.backfills, trigger)
	hold := s.hold
	s.mu.Unlock()
	if runner == nil {
		return errors.New("the backfill should be handed the runtime's runner")
	}
	if hold != nil {
		<-hold
	}
	return nil
}

func (s *recordingGraphSync) backfillTriggers() []sdd.SyncTrigger {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.backfills)
}

// awaitBackfills waits until the backfills started in the background number
// want, and fails the test when they do not.
func awaitBackfills(t *testing.T, sync *recordingGraphSync, want int) []sdd.SyncTrigger {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		triggers := sync.backfillTriggers()
		if len(triggers) >= want {
			return triggers
		}
		select {
		case <-deadline:
			t.Fatalf("a title backfill should start in the background, got %v", triggers)
		case <-time.After(time.Millisecond):
		}
	}
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

func (s *recordingGraphSync) ReleaseSyncOwner(_ context.Context, _, session string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.released = append(s.released, session)
	return s.notice, nil
}

func (s *recordingGraphSync) SyncDue(context.Context, string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dueAsked++
	return s.due, nil
}

func (s *recordingGraphSync) dueChecks() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dueAsked
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
	t.Run("a tick runs a due round", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			sync := &recordingGraphSync{due: true}
			_, runtime := syncedApplication(t, sync)
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error)
			go func() { done <- runtime.RunSyncTimer(ctx) }()

			time.Sleep(time.Minute + time.Second)
			synctest.Wait()
			if rounds := sync.rounds(); !slices.Equal(rounds, []sdd.SyncTrigger{sdd.SyncOnTimer}) {
				t.Errorf("the first tick should run one timer round, got %v", rounds)
			}
			time.Sleep(time.Minute)
			synctest.Wait()
			if len(sync.rounds()) != 2 {
				t.Errorf("every tick finding the round due should run it, got %v", sync.rounds())
			}
			cancel()
			err := <-done
			if err != nil {
				t.Fatal(err)
			}
			stopped := len(sync.rounds())
			time.Sleep(5 * time.Minute)
			synctest.Wait()
			if len(sync.rounds()) != stopped {
				t.Error("no round should run after the timer stopped")
			}
		})
	})
	t.Run("a tick runs nothing not due", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			sync := &recordingGraphSync{}
			_, runtime := syncedApplication(t, sync)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			go func() { _ = runtime.RunSyncTimer(ctx) }()

			time.Sleep(3*time.Minute + time.Second)
			synctest.Wait()
			if sync.dueChecks() != 3 {
				t.Errorf("each minute should ask whether the round is due, got %d asks", sync.dueChecks())
			}
			if len(sync.rounds()) != 0 || len(sync.backfillTriggers()) != 0 {
				t.Error("a round not due should not run, nor its backfill")
			}
		})
	})
}

func (s *recordingGraphSync) SyncBlocked(context.Context, string) (*sdd.SyncBlocked, error) {
	return s.blocked, nil
}

func TestSessionStartBackfillsTitlesWithoutWaiting(t *testing.T) {
	sync := &recordingGraphSync{hold: make(chan struct{})}
	defer close(sync.hold)
	application, _ := syncedApplication(t, sync)
	identity := sdd.RequestIdentity{Subject: "christopher"}

	opened := make(chan error, 1)
	go func() {
		_, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "test"})
		opened <- err
	}()
	select {
	case err := <-opened:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the session should be served while its title backfill still runs")
	}
	if triggers := awaitBackfills(t, sync, 1); !slices.Equal(triggers, []sdd.SyncTrigger{sdd.SyncOnSessionStart}) {
		t.Errorf("a session start should start one backfill, got %v", triggers)
	}
	if len(sync.rounds()) != 0 {
		t.Error("a session start should run no sync round of its own")
	}
}

func TestRefreshWorkflowBackfillsTitlesAfterItsRound(t *testing.T) {
	sync := &recordingGraphSync{}
	application, _ := syncedApplication(t, sync)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	workflow, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "test"})
	if err != nil {
		t.Fatal(err)
	}
	awaitBackfills(t, sync, 1)
	_, err = application.RefreshWorkflow(t.Context(), identity, workflow.ID())
	if err != nil {
		t.Fatal(err)
	}
	if triggers := awaitBackfills(t, sync, 2); triggers[1] != sdd.SyncOnCall {
		t.Errorf("an engine call should start a backfill bound by the sync cooldown, got %v", triggers)
	}
}

func TestSyncTimerBackfillsTitlesEachRound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sync := &recordingGraphSync{due: true}
		_, runtime := syncedApplication(t, sync)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { _ = runtime.RunSyncTimer(ctx) }()

		time.Sleep(2*time.Minute + time.Second)
		synctest.Wait()
		triggers := sync.backfillTriggers()
		if len(triggers) != 2 {
			t.Fatalf("each due round should start a title backfill, got %v", triggers)
		}
		for _, trigger := range triggers {
			if trigger != sdd.SyncOnTimer {
				t.Errorf("a timer round should start a backfill that always runs, got %q", trigger)
			}
		}
	})
}
