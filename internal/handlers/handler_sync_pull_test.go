package handlers_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/handlers/synctest"
	"github.com/networkteam/sdd/internal/repos"
)

// fakePuller scripts the git surface the sync-pull handler consumes and
// records whether MergePull was reached.
type fakePuller struct {
	clean       bool
	cleanErr    error
	pullOut     string
	pullErr     error
	mergeCalled bool
	url         string
}

func (f *fakePuller) UpstreamURL(context.Context) (string, error) {
	if f.url == "" {
		return "/fake/remote.git", nil
	}
	return f.url, nil
}

func (f *fakePuller) IsClean(context.Context) (bool, error) {
	return f.clean, f.cleanErr
}

func (f *fakePuller) MergePull(context.Context) (string, error) {
	f.mergeCalled = true
	return f.pullOut, f.pullErr
}

func TestSyncPull_CleanTreeMergesAndReports(t *testing.T) {
	puller := &fakePuller{clean: true, pullOut: "Already up to date."}
	h := handlers.New(handlers.Options{Puller: puller, Gate: synctest.Gate(t.TempDir(), nil)})

	var got string
	cmd := &command.SyncPullCmd{OnPulled: func(output string) { got = output }}

	if err := h.SyncPull(context.Background(), cmd); err != nil {
		t.Fatalf("SyncPull: %v", err)
	}
	if !puller.mergeCalled {
		t.Error("MergePull should have been called on a clean tree")
	}
	if got != "Already up to date." {
		t.Errorf("OnPulled output = %q, want %q", got, "Already up to date.")
	}
}

func TestSyncPull_DirtyTreeRefusesWithoutPulling(t *testing.T) {
	puller := &fakePuller{clean: false}
	h := handlers.New(handlers.Options{Puller: puller, Gate: synctest.Gate(t.TempDir(), nil)})

	called := false
	cmd := &command.SyncPullCmd{OnPulled: func(string) { called = true }}

	err := h.SyncPull(context.Background(), cmd)
	if err == nil {
		t.Fatal("SyncPull should refuse on a dirty working tree")
	}
	if !strings.Contains(err.Error(), "uncommitted changes") {
		t.Errorf("error = %q, want it to mention uncommitted changes", err.Error())
	}
	if puller.mergeCalled {
		t.Error("MergePull must not run on a dirty tree")
	}
	if called {
		t.Error("OnPulled must not fire when the pull is refused")
	}
}

func TestSyncPull_NilPullerErrors(t *testing.T) {
	h := handlers.New(handlers.Options{})
	if err := h.SyncPull(context.Background(), &command.SyncPullCmd{}); err == nil {
		t.Fatal("SyncPull should error when no Puller is configured")
	}
}

func TestSyncPull_PropagatesPullFailure(t *testing.T) {
	puller := &fakePuller{clean: true, pullErr: errors.New("network down")}
	h := handlers.New(handlers.Options{Puller: puller, Gate: synctest.Gate(t.TempDir(), nil)})

	err := h.SyncPull(context.Background(), &command.SyncPullCmd{})
	if err == nil {
		t.Fatal("SyncPull should propagate a pull failure")
	}
	if !strings.Contains(err.Error(), "network down") {
		t.Errorf("error = %q, want it to wrap the pull failure", err.Error())
	}
}

func TestSyncPull_DeferredWithinTheCooldown(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	puller := &fakePuller{clean: true, url: "git@example.test:acme/graph.git"}
	h := handlers.New(handlers.Options{Puller: puller, Gate: synctest.Gate(t.TempDir(), func() time.Time { return now })})

	err := h.SyncPull(context.Background(), &command.SyncPullCmd{})
	if err != nil {
		t.Fatalf("the first pull should run: %v", err)
	}
	puller.mergeCalled = false
	var next time.Time
	pulled := false
	err = h.SyncPull(context.Background(), &command.SyncPullCmd{
		OnPulled:   func(string) { pulled = true },
		OnDeferred: func(at time.Time) { next = at },
	})
	if err != nil {
		t.Fatalf("a deferred pull should be a skip, not a failure: %v", err)
	}
	if puller.mergeCalled || pulled {
		t.Error("a pull within the fetch cooldown should not run")
	}
	if !next.After(now) {
		t.Errorf("the deferral should name a later time it may run, got %v", next)
	}
}

func TestSyncPull_WithoutGateRefuses(t *testing.T) {
	puller := &fakePuller{clean: true}
	h := handlers.New(handlers.Options{Puller: puller})

	err := h.SyncPull(context.Background(), &command.SyncPullCmd{})
	if !errors.Is(err, repos.ErrNoGate) || puller.mergeCalled {
		t.Errorf("a pull without a gate should be refused, got %v", err)
	}
}
