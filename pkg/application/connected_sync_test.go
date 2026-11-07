package application_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

// scriptedConnected records each request and answers with the outcomes
// queued for it, the last one repeated.
type scriptedConnected struct {
	mu       sync.Mutex
	requests []sdd.SyncReposRequest
	answers  [][]sdd.RepoSyncOutcome
}

func (c *scriptedConnected) SyncRepos(_ context.Context, request sdd.SyncReposRequest) ([]sdd.RepoSyncOutcome, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, request)
	if len(c.answers) == 0 {
		return nil, nil
	}
	answer := c.answers[0]
	if len(c.answers) > 1 {
		c.answers = c.answers[1:]
	}
	return answer, nil
}

func (c *scriptedConnected) asked() []sdd.SyncReposRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.requests)
}

func connectedApplication(t *testing.T, connected sdd.ConnectedSync, graphSync sdd.GraphSync) (*sdd.Application, *sdd.ProjectRuntime, *runtimeAccessResolver) {
	t.Helper()
	graph, err := localadapter.NewFilesystemGraphStore(localadapter.FilesystemGraphStoreOptions{Project: "example", GraphDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := sdd.NewProjectRuntime(sdd.ProjectRuntimeOptions{
		Project: sdd.ProjectRef{ID: "example"}, DefaultBranch: "main", Graph: graph, Sync: graphSync, Connected: connected,
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
	access := &runtimeAccessResolver{runtime: runtime}
	application, err := sdd.NewApplication(sdd.ApplicationOptions{Access: access, Sessions: sessions, StagedBlobs: blobs})
	if err != nil {
		t.Fatal(err)
	}
	return application, runtime, access
}

// Each tick of a running server asks for the due rounds of the connected
// repositories, whether or not the own graph syncs automatically.
func TestSyncTimerRunsTheDueConnectedRepos(t *testing.T) {
	cases := []struct {
		name      string
		graphSync sdd.GraphSync
	}{
		{name: "without automatic sync of the own graph"},
		{name: "beside an own graph not due", graphSync: &recordingGraphSync{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				connected := &scriptedConnected{}
				_, runtime, _ := connectedApplication(t, connected, tc.graphSync)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				go func() { _ = runtime.RunSyncTimer(ctx) }()

				time.Sleep(2*time.Minute + time.Second)
				synctest.Wait()
				asked := connected.asked()
				if len(asked) != 2 || !asked[0].Due || len(asked[0].Repos) != 0 {
					t.Errorf("each tick should ask for the due rounds of the connected repos, got %+v", asked)
				}
			})
		})
	}
}

// A connected repo whose round failed shows in the sync notice until a round
// of it succeeds; a deferral stays silent.
func TestConnectedFailureReachesTheSyncNotice(t *testing.T) {
	retry := time.Date(2026, 11, 7, 12, 5, 0, 0, time.UTC)
	connected := &scriptedConnected{answers: [][]sdd.RepoSyncOutcome{
		{{Repo: "example.test/acme/other", State: sdd.RepoFailed, Reason: "remote unreachable", Next: retry}, {Repo: "example.test/acme/later", State: sdd.RepoSkipped, Next: retry}},
		{{Repo: "example.test/acme/other", State: sdd.RepoSynced}},
	}}
	application, _, access := connectedApplication(t, connected, nil)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	workflow, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "test"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = application.SyncRepos(t.Context(), identity, "example", sdd.SyncReposRequest{})
	if err != nil {
		t.Fatal(err)
	}
	notice := workflow.SyncNotice(t.Context(), identity)
	if !strings.Contains(notice, "example.test/acme/other failed: remote unreachable") || !strings.Contains(notice, "retried from") {
		t.Errorf("the notice should name the failed repo, why, and when it is retried, got %q", notice)
	}
	if strings.Contains(notice, "example.test/acme/later") {
		t.Errorf("a deferral should stay out of the notice, got %q", notice)
	}

	result, err := application.SyncRepos(t.Context(), identity, "example", sdd.SyncReposRequest{Repos: []sdd.RepoID{"example.test/acme/other"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Outcomes) != 1 || result.Outcomes[0].State != sdd.RepoSynced {
		t.Errorf("the outcomes should be returned as the sync reported them, got %+v", result.Outcomes)
	}
	if notice := workflow.SyncNotice(t.Context(), identity); notice != "" {
		t.Errorf("a successful round should clear the failure, got %q", notice)
	}
	if !slices.Contains(access.accesses, sdd.AccessWrite) {
		t.Error("a sync merges into the clones and pushes, so it should ask for write access")
	}
	if asked := connected.asked(); len(asked) != 2 || !slices.Equal(asked[1].Repos, []sdd.RepoID{"example.test/acme/other"}) {
		t.Errorf("the named repos should reach the sync, got %+v", asked)
	}
}

// A runtime that syncs no connected repos refuses the request.
func TestSyncReposWithoutConnectedRepos(t *testing.T) {
	application, _, _ := connectedApplication(t, nil, nil)
	_, err := application.SyncRepos(t.Context(), sdd.RequestIdentity{Subject: "christopher"}, "example", sdd.SyncReposRequest{})
	var coded *sdd.ApplicationError
	if !errors.As(err, &coded) || coded.Code != sdd.ErrorProjectUnavailable {
		t.Errorf("the request should be refused as unavailable, got %v", err)
	}
}
