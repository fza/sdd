package cliapp_test

import (
	"strings"
	"testing"
)

// Bare `sdd sync` in a code repository fetches and prints where the graph
// stands against its upstream, leaving the merge to --pull.
func TestBareSyncFetchesAndReportsTheStatus(t *testing.T) {
	root := syncedProject(t, "")
	incoming := upstreamCommit(t, root, "sdd: signal tactical Another participant's entry", ".sdd/graph/incoming.md")

	out, err := runAppErr(t, root, "sync")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	if strings.Count(out, "sync: fast-forward available, 1 commits behind") != 1 {
		t.Errorf("the fetch should report the incoming commit once, got %q", out)
	}
	if contains(t, root, incoming) {
		t.Error("a bare sync should merge nothing")
	}
}

// A fetch the git gate defers is reported with when it may run, beside the
// status of what was fetched before.
func TestBareSyncReportsADeferredFetch(t *testing.T) {
	root := syncedProject(t, "")
	upstreamCommit(t, root, "sdd: signal tactical Another participant's entry", ".sdd/graph/incoming.md")
	_, err := runAppErr(t, root, "sync")
	if err != nil {
		t.Fatal(err)
	}
	deferFetches(t, root)

	out, err := runAppErr(t, root, "sync")
	if err != nil {
		t.Fatalf("a deferred fetch should be a skip, not a failure: %v\n%s", err, out)
	}
	if !strings.Contains(out, "sync: fetch skipped, next allowed at") || !strings.Contains(out, "sync: fast-forward available, 1 commits behind") {
		t.Errorf("the sync should say it skipped the fetch and where the graph stands, got %q", out)
	}
}

// A repository the fetch cannot reach its upstream from fails the sync,
// naming why.
func TestBareSyncFailsWithoutAnUpstream(t *testing.T) {
	root := syncedProject(t, "")
	gitIn(t, root, "branch", "--unset-upstream")

	out, err := runAppErr(t, root, "sync")
	if err == nil || !strings.Contains(err.Error(), "no upstream") {
		t.Errorf("a sync without an upstream should fail naming it, got %v\n%s", err, out)
	}
}
