package mcpapp_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// testConnectedSync answers every sync with its scripted outcomes and keeps
// the requests.
type testConnectedSync struct {
	mu       sync.Mutex
	outcomes []sdd.RepoSyncOutcome
	requests []sdd.SyncReposRequest
}

func (c *testConnectedSync) SyncRepos(_ context.Context, request sdd.SyncReposRequest) ([]sdd.RepoSyncOutcome, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, request)
	return c.outcomes, nil
}

// sync_repos returns one structured outcome per connected repo, passes the
// named repos on, and a failure stays in the sync notice of later calls.
func TestSyncReposReportsEachRepo(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	next := time.Date(2026, 11, 7, 12, 5, 0, 0, time.UTC)
	env.connected.outcomes = []sdd.RepoSyncOutcome{
		{Repo: "example.test/acme/quiet", State: sdd.RepoSynced},
		{Repo: "example.test/acme/later", State: sdd.RepoSkipped, Next: next},
		{Repo: "example.test/acme/offline", State: sdd.RepoFailed, Reason: "remote unreachable", Next: next},
	}
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session

	var result mcpserver.SyncReposResult
	call(t, cs, "sync_repos", map[string]any{"session": session, "repos": []string{"example.test/acme/quiet", "example.test/acme/later", "example.test/acme/offline"}}, &result)
	want := []mcpserver.RepoSyncResult{
		{Repo: "example.test/acme/quiet", State: "synced"},
		{Repo: "example.test/acme/later", State: "skipped", Next: next.Format(time.RFC3339)},
		{Repo: "example.test/acme/offline", State: "failed", Next: next.Format(time.RFC3339), Reason: "remote unreachable"},
	}
	if len(result.Repos) != len(want) {
		t.Fatalf("one outcome per repo should be returned, got %+v", result.Repos)
	}
	for i := range want {
		if result.Repos[i] != want[i] {
			t.Errorf("outcome %d should carry its state, time and reason, got %+v", i, result.Repos[i])
		}
	}
	if !strings.Contains(result.SyncNotice, "example.test/acme/offline failed") {
		t.Errorf("the failure should reach the sync notice, got %q", result.SyncNotice)
	}
	if len(env.connected.requests) != 1 || len(env.connected.requests[0].Repos) != 3 || env.connected.requests[0].Due {
		t.Errorf("the named repos should reach the sync, got %+v", env.connected.requests)
	}

	var info mcpserver.InfoResult
	call(t, cs, "info", map[string]any{"session": session}, &info)
	if !strings.Contains(info.SyncNotice, "example.test/acme/offline failed") {
		t.Errorf("the failure should stay in the notice of later calls, got %q", info.SyncNotice)
	}
}
