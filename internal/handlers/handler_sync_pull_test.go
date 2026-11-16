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

	fetchedOut    string
	fetchedMerged bool
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

func (f *fakePuller) MergeFetched(context.Context) (string, error) {
	f.fetchedMerged = true
	return f.fetchedOut, f.pullErr
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
	puller.fetchedOut = "Merge made by the 'ort' strategy."
	var next time.Time
	var pulled string
	err = h.SyncPull(context.Background(), &command.SyncPullCmd{
		OnPulled:   func(out string) { pulled = out },
		OnDeferred: func(at time.Time) { next = at },
	})
	if err != nil {
		t.Fatalf("a pull within the fetch cooldown should still merge: %v", err)
	}
	if puller.mergeCalled {
		t.Error("a pull within the fetch cooldown should not fetch")
	}
	if !puller.fetchedMerged || pulled != puller.fetchedOut {
		t.Errorf("a pull within the fetch cooldown should merge what was fetched and report it, got %q", pulled)
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

// syncLockSpy is a sync lock that reports whether it is held.
type syncLockSpy struct{ held bool }

func (s *syncLockSpy) TryLock(context.Context) (func(), error) { return s.take(), nil }

func (s *syncLockSpy) Lock(context.Context) (func(), error) { return s.take(), nil }

func (s *syncLockSpy) take() func() {
	s.held = true
	return func() { s.held = false }
}

// lockCheckingPuller records whether the sync lock was held during the merge.
type lockCheckingPuller struct {
	fakePuller
	lock         *syncLockSpy
	heldForMerge bool
}

func (p *lockCheckingPuller) MergePull(ctx context.Context) (string, error) {
	p.heldForMerge = p.lock.held
	return p.fakePuller.MergePull(ctx)
}

// A pull merges under the sync lock, so no sync round of another process
// merges or keeps the graph clone beside it.
func TestSyncPull_MergesUnderTheSyncLock(t *testing.T) {
	lock := &syncLockSpy{}
	puller := &lockCheckingPuller{fakePuller: fakePuller{clean: true}, lock: lock}
	h := handlers.New(handlers.Options{Puller: puller, SyncLock: lock, Gate: synctest.Gate(t.TempDir(), nil)})

	err := h.SyncPull(context.Background(), &command.SyncPullCmd{})
	if err != nil {
		t.Fatalf("SyncPull: %v", err)
	}
	if !puller.heldForMerge {
		t.Error("the merge should run under the sync lock")
	}
	if lock.held {
		t.Error("the pull should release the sync lock")
	}
}
