package application_test

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

// leaseFixture is newClosureFixture's composition with a recording graph
// sync per project: project-a is the home and declares project-b under
// example.test/b.
type leaseFixture struct {
	application *sdd.Application
	alice       sdd.RequestIdentity
	home, dep   *recordingGraphSync
}

func newLeaseFixture(t *testing.T) leaseFixture {
	t.Helper()
	root := t.TempDir()
	llm := pkgllm.RunnerFunc(func(context.Context, pkgllm.Request) (pkgllm.Result, error) {
		return pkgllm.Result{Text: `{"findings":[]}`, Identity: pkgllm.Identity{Provider: "test", Model: "test"}}, nil
	})
	f := leaseFixture{alice: sdd.RequestIdentity{Subject: "alice"}, home: &recordingGraphSync{}, dep: &recordingGraphSync{}}
	runtimes := map[sdd.ProjectID]*sdd.ProjectRuntime{}
	for _, project := range []struct {
		id           sdd.ProjectID
		dependencies []sdd.RepoID
		sync         *recordingGraphSync
	}{
		{id: "project-a", dependencies: []sdd.RepoID{"example.test/b"}, sync: f.home},
		{id: "project-b", sync: f.dep},
	} {
		graph, err := localadapter.NewFilesystemGraphStore(localadapter.FilesystemGraphStoreOptions{Project: project.id, GraphDir: filepath.Join(root, string(project.id))})
		if err != nil {
			t.Fatal(err)
		}
		runtime, err := sdd.NewProjectRuntime(sdd.ProjectRuntimeOptions{
			Project: sdd.ProjectRef{ID: project.id, DisplayName: string(project.id)}, Dependencies: project.dependencies,
			DefaultBranch: "main", Graph: graph, LLM: llm, Sync: project.sync,
		})
		if err != nil {
			t.Fatal(err)
		}
		runtimes[project.id] = runtime
	}
	sessions, err := localadapter.NewFilesystemSessionStoreAt(filepath.Join(root, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := localadapter.NewFilesystemStagedBlobStoreAt(filepath.Join(root, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	access := &compositionAccess{
		runtimes: runtimes,
		permissions: map[string]map[sdd.ProjectID]compositionPermission{
			"alice": {"project-a": {read: true, write: true}, "project-b": {read: true, write: true}},
		},
		dependencies: map[sdd.RepoID]sdd.ProjectID{"example.test/b": "project-b"},
	}
	f.application, err = sdd.NewApplication(sdd.ApplicationOptions{Access: access, Sessions: sessions, StagedBlobs: blobs})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f leaseFixture) open(t *testing.T) *sdd.WorkflowSession {
	t.Helper()
	workflow, _, err := f.application.OpenWorkflow(t.Context(), f.alice, "project-a", sdd.WorkflowOpenRequest{ClientName: "test"})
	if err != nil {
		t.Fatal(err)
	}
	f.home.clearDemands()
	f.dep.clearDemands()
	return workflow
}

func TestOpeningASessionRenewsTheHomeLease(t *testing.T) {
	f := newLeaseFixture(t)
	if _, _, err := f.application.OpenWorkflow(t.Context(), f.alice, "project-a", sdd.WorkflowOpenRequest{ClientName: "test"}); err != nil {
		t.Fatal(err)
	}
	if got := f.home.demanded(); !slices.Equal(got, []string{"main"}) {
		t.Errorf("start_session should renew the home graph's lease on its branch, got %v", got)
	}
	if got := f.dep.demanded(); len(got) != 0 {
		t.Errorf("start_session should leave a dependency's lease alone, got %v", got)
	}
}

func TestCallsRenewTheLeaseOfTheProjectTheyWorkIn(t *testing.T) {
	for _, tc := range []struct {
		name      string
		call      func(t *testing.T, f leaseFixture, workflow *sdd.WorkflowSession) error
		home, dep int
	}{
		{name: "resume_session", home: 1, call: func(t *testing.T, f leaseFixture, workflow *sdd.WorkflowSession) error {
			return workflow.Reorient(t.Context(), f.alice)
		}},
		{name: "start_procedure at home", home: 1, call: func(t *testing.T, f leaseFixture, workflow *sdd.WorkflowSession) error {
			_, err := workflow.Start(t.Context(), f.alice, sdd.WorkflowStartRequest{Canonical: "capture"})
			return err
		}},
		{name: "start_procedure in a dependency", dep: 1, call: func(t *testing.T, f leaseFixture, workflow *sdd.WorkflowSession) error {
			_, err := workflow.Start(t.Context(), f.alice, sdd.WorkflowStartRequest{Canonical: "capture", Project: "project-b"})
			return err
		}},
		{name: "next on an instance in a dependency", dep: 1, call: func(t *testing.T, f leaseFixture, workflow *sdd.WorkflowSession) error {
			serve, err := workflow.Start(t.Context(), f.alice, sdd.WorkflowStartRequest{Canonical: "capture", Project: "project-b"})
			if err != nil {
				return err
			}
			f.dep.clearDemands()
			_, err = workflow.Advance(t.Context(), f.alice, sdd.WorkflowAdvanceRequest{Instance: serve.Instance})
			return err
		}},
		{name: "park of an instance in a dependency", dep: 1, call: func(t *testing.T, f leaseFixture, workflow *sdd.WorkflowSession) error {
			serve, err := workflow.Start(t.Context(), f.alice, sdd.WorkflowStartRequest{Canonical: "capture", Project: "project-b"})
			if err != nil {
				return err
			}
			f.dep.clearDemands()
			_, err = workflow.Park(t.Context(), f.alice, serve.Instance, "later")
			return err
		}},
		{name: "abandon of an instance in a dependency", dep: 1, call: func(t *testing.T, f leaseFixture, workflow *sdd.WorkflowSession) error {
			serve, err := workflow.Start(t.Context(), f.alice, sdd.WorkflowStartRequest{Canonical: "capture", Project: "project-b"})
			if err != nil {
				return err
			}
			f.dep.clearDemands()
			_, err = workflow.Abandon(t.Context(), f.alice, serve.Instance, "dropped")
			return err
		}},
		{name: "gather for an instance in a dependency", dep: 1, call: func(t *testing.T, f leaseFixture, workflow *sdd.WorkflowSession) error {
			serve, err := workflow.Start(t.Context(), f.alice, sdd.WorkflowStartRequest{Canonical: "capture", Project: "project-b"})
			if err != nil {
				return err
			}
			f.dep.clearDemands()
			_, err = workflow.Gather(t.Context(), f.alice, sdd.WorkflowGatherRequest{Instance: serve.Instance, Draft: "A fact about the dependency.", SyncMode: sdd.SearchSyncNone})
			return err
		}},
		{name: "read naming no project", home: 1, call: func(t *testing.T, f leaseFixture, workflow *sdd.WorkflowSession) error {
			_, _, _, err := workflow.ReadScope(t.Context(), f.alice, "")
			return err
		}},
		{name: "read naming a dependency", dep: 1, call: func(t *testing.T, f leaseFixture, workflow *sdd.WorkflowSession) error {
			_, _, _, err := workflow.ReadScope(t.Context(), f.alice, "project-b")
			return err
		}},
		{name: "view naming a dependency", dep: 1, call: func(t *testing.T, f leaseFixture, _ *sdd.WorkflowSession) error {
			_, err := f.application.View(t.Context(), f.alice, "project-a", sdd.ViewRequest{Layout: "active:as-counts", Repos: []sdd.RepoID{"example.test/b"}})
			return err
		}},
		{name: "a capture ref into a dependency", dep: 1, call: func(t *testing.T, f leaseFixture, workflow *sdd.WorkflowSession) error {
			_, err := f.application.PreflightEntry(t.Context(), f.alice, "project-a", workflow.Binding(), sdd.EntryDraft{
				Kind: "fact", Layer: "tactical", Confidence: "high", Body: "A fact about the dependency.",
				Refs: []sdd.EntryRef{{ID: "example.test/b:20260713-120000-s-tac-dep", Kind: "related", Desc: "the dependency's fact"}},
			})
			return err
		}},
		{name: "a read of the registry", call: func(t *testing.T, f leaseFixture, _ *sdd.WorkflowSession) error {
			_, err := f.application.Procedures(t.Context(), f.alice, "project-a", sdd.ProcedureListRequest{})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newLeaseFixture(t)
			workflow := f.open(t)
			if err := tc.call(t, f, workflow); err != nil {
				t.Fatal(err)
			}
			if got := f.home.demanded(); len(got) != tc.home {
				t.Errorf("the home graph's lease should be renewed %d times, got %v", tc.home, got)
			}
			if got := f.dep.demanded(); len(got) != tc.dep {
				t.Errorf("the dependency's lease should be renewed %d times, got %v", tc.dep, got)
			}
		})
	}
}

func TestRefreshingASessionRunsNoSyncRound(t *testing.T) {
	f := newLeaseFixture(t)
	workflow := f.open(t)
	if _, err := f.application.RefreshWorkflow(t.Context(), f.alice, workflow.ID()); err != nil {
		t.Fatal(err)
	}
	if len(f.home.rounds()) != 0 || len(f.home.demanded()) != 0 {
		t.Errorf("replaying a session for a call should neither sync nor renew a lease, got rounds %v, demands %v", f.home.rounds(), f.home.demanded())
	}
}

func TestARefreshPastTheReadWaitShowsInTheSyncNotice(t *testing.T) {
	f := newLeaseFixture(t)
	fetched := time.Now().Add(-42 * time.Minute)
	f.dep.demand = sdd.GraphDemand{Ran: true, Pending: true, Fetched: fetched}
	workflow := f.open(t)
	if _, _, _, err := workflow.ReadScope(t.Context(), f.alice, "project-b"); err != nil {
		t.Fatal(err)
	}
	notice := workflow.SyncNotice(t.Context(), f.alice)
	if !strings.Contains(notice, "refresh of project-b still running") || !strings.Contains(notice, "42m0s ago") {
		t.Errorf("the notice should say the refresh still runs and how old the clone is, got %q", notice)
	}

	done := make(chan error)
	go func() { done <- f.application.Shutdown(t.Context()) }()
	select {
	case err := <-done:
		t.Fatalf("shutdown should wait for the refresh still running, returned %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	f.dep.settle()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if notice := workflow.SyncNotice(t.Context(), f.alice); notice != "" {
		t.Errorf("a settled refresh should leave the notice, got %q", notice)
	}
}

func TestARefreshSettledInTimeStaysOutOfTheNotice(t *testing.T) {
	f := newLeaseFixture(t)
	f.dep.demand = sdd.GraphDemand{Ran: true}
	workflow := f.open(t)
	if _, _, _, err := workflow.ReadScope(t.Context(), f.alice, "project-b"); err != nil {
		t.Fatal(err)
	}
	if notice := workflow.SyncNotice(t.Context(), f.alice); notice != "" {
		t.Errorf("a refresh finished within the read wait should not show, got %q", notice)
	}
}

// demandRecord is the part of recordingGraphSync DemandGraph keeps.
type demandRecord struct {
	mu       sync.Mutex
	branches []string
	settled  []func()
}

func (s *recordingGraphSync) DemandGraph(_ context.Context, branch string, settled func()) (sdd.GraphDemand, error) {
	s.leases.mu.Lock()
	defer s.leases.mu.Unlock()
	s.leases.branches = append(s.leases.branches, branch)
	if s.demand.Pending {
		s.leases.settled = append(s.leases.settled, settled)
	}
	return s.demand, s.err
}

func (s *recordingGraphSync) demanded() []string {
	s.leases.mu.Lock()
	defer s.leases.mu.Unlock()
	return slices.Clone(s.leases.branches)
}

func (s *recordingGraphSync) clearDemands() {
	s.leases.mu.Lock()
	defer s.leases.mu.Unlock()
	s.leases.branches = nil
}

// settle ends every round reported pending.
func (s *recordingGraphSync) settle() {
	s.leases.mu.Lock()
	settled := s.leases.settled
	s.leases.settled = nil
	s.leases.mu.Unlock()
	for _, end := range settled {
		end()
	}
}
