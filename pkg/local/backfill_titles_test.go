package local_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/syncstate"
	sdd "github.com/networkteam/sdd/pkg/application"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
)

func untitledEntry(id string) (path, content string) {
	return fmt.Sprintf(".sdd/graph/%s/%s/%s.md", id[:4], id[4:6], id[6:]),
		fmt.Sprintf("---\ntype: signal\nlayer: tactical\nkind: gap\nsummary: Summary of %s.\n---\n\nBody of %s.\n", id, id)
}

// titleOnlyRunner titles every entry it is asked about; started, when set,
// receives each call and release holds it until closed.
type titleOnlyRunner struct {
	started chan struct{}
	release chan struct{}
	err     error
}

func (r titleOnlyRunner) Run(ctx context.Context, request pkgllm.Request) (pkgllm.Result, error) {
	if r.started != nil {
		r.started <- struct{}{}
		select {
		case <-r.release:
		case <-ctx.Done():
			return pkgllm.Result{}, ctx.Err()
		}
	}
	if r.err != nil {
		return pkgllm.Result{}, r.err
	}
	answer := map[string]string{"title": "backfilled title"}
	if request.Purpose == pkgllm.PurposeSummarize {
		answer["summary"] = "Backfilled summary."
	}
	data, err := json.Marshal(answer)
	return pkgllm.Result{Text: string(data)}, err
}

func TestSyncRoundPullsUntitledEntriesAndTheBackfillTitlesThem(t *testing.T) {
	repo, remote := syncedRepository(t, "auto")
	existingPath, existing := untitledEntry("20261101-100000-s-tac-old")
	repo.write(existingPath, existing)
	repo.git("add", existingPath)
	repo.git("commit", "-m", "sdd: signal tactical gap")
	pulledPath, pulled := untitledEntry("20261101-110000-s-tac-new")
	pushUpstream(t, remote, pulledPath, pulled, "sdd: signal tactical gap")

	targets := repo.targets()
	err := targets.SyncGraph(t.Context(), "main", sdd.SyncOnCall)
	if err != nil {
		t.Fatal(err)
	}
	err = targets.BackfillTitles(t.Context(), "main", sdd.SyncOnCall, titleOnlyRunner{})
	if err != nil {
		t.Fatal(err)
	}

	if subject := repo.git("log", "-1", "--format=%s"); subject != "sdd: summarize 2 entries" {
		t.Errorf("the pulled and the existing untitled entry should be titled in one commit, got %q", subject)
	}
	for _, path := range []string{existingPath, pulledPath} {
		if !strings.Contains(repo.git("show", "HEAD:"+path), "title: Backfilled title") {
			t.Errorf("%s should be committed with its title", path)
		}
	}
	if status := repo.git("status", "--porcelain"); status != "" {
		t.Errorf("the backfill should leave nothing uncommitted, got %q", status)
	}
	err = targets.SyncGraph(t.Context(), "main", sdd.SyncOnTimer)
	if err != nil {
		t.Fatal(err)
	}
	if gitIn(t, "", "-C", remote, "rev-parse", "main") != repo.git("rev-parse", "HEAD") {
		t.Error("the next round should push the titles")
	}
}

func TestBackfillTitlesRunsUnderManualSync(t *testing.T) {
	repo, _ := syncedRepository(t, "manual")
	path, content := untitledEntry("20261101-100000-s-tac-man")
	repo.write(path, content)
	repo.git("add", path)
	repo.git("commit", "-m", "sdd: signal tactical gap")
	targets := repo.targets()

	err := targets.BackfillTitles(t.Context(), "main", sdd.SyncOnSessionStart, titleOnlyRunner{err: errors.New("no route")})
	if err != nil {
		t.Fatal(err)
	}
	notice, err := targets.SyncNotice(t.Context(), "main", "s_1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(notice, "title backfill failing since") {
		t.Errorf("under manual sync the failed backfill should be served as the sync notice, got %q", notice)
	}
	blocked, err := targets.SyncBlocked(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if blocked == nil || blocked.Kind != "titles-failed" {
		t.Errorf("under manual sync info should show the failed backfill, got %+v", blocked)
	}

	err = targets.BackfillTitles(t.Context(), "main", sdd.SyncOnSessionStart, titleOnlyRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if subject := repo.git("log", "-1", "--format=%s"); subject != "sdd: summarize 1 entries" {
		t.Errorf("under manual sync the backfill should title and commit, got %q", subject)
	}
	if notice, _ := targets.SyncNotice(t.Context(), "main", "s_1"); notice != "" {
		t.Errorf("a backfill that succeeds should clear the notice, got %q", notice)
	}
}

func TestBackfillAndCaptureInOneCheckoutBothCommit(t *testing.T) {
	repo, _ := syncedRepository(t, "manual")
	path, content := untitledEntry("20261101-100000-s-tac-cap")
	repo.write(path, content)
	repo.git("add", path)
	repo.git("commit", "-m", "sdd: signal tactical gap")

	runner := titleOnlyRunner{started: make(chan struct{}, 1), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- repo.targets().BackfillTitles(context.WithoutCancel(t.Context()), "main", sdd.SyncOnSessionStart, runner)
	}()
	<-runner.started

	application, identity, binding := repo.captureApplication(t, summaryRunner())
	created, err := application.CreateEntry(t.Context(), identity, "example", binding, captureDraft(binding, "20261101-120000-s-tac-cpt"))
	if err != nil {
		t.Fatal(err)
	}
	close(runner.release)
	err = <-done
	if err != nil {
		t.Fatal(err)
	}

	capturedPath, _ := untitledEntry(created.EntryID)
	subjects := repo.git("log", "-2", "--format=%s")
	captureCommits := repo.git("log", "--format=%h", "HEAD~2..HEAD", "--", capturedPath)
	if !strings.Contains(subjects, "sdd: summarize 1 entries") || strings.TrimSpace(captureCommits) == "" {
		t.Errorf("the capture and the backfill should each commit, got %q", subjects)
	}
	if !strings.Contains(repo.git("show", "HEAD:"+capturedPath), "Engine capture under sync.") {
		t.Error("the capture's entry should stay committed")
	}
	if !strings.Contains(repo.git("show", "HEAD:"+path), "title: Backfilled title") {
		t.Error("the backfill's title should be committed")
	}
	if status := repo.git("status", "--porcelain"); status != "" {
		t.Errorf("neither write should be left uncommitted, got %q", status)
	}
}

func TestBackfillTitlesWaitsOutTheCooldownOnCallsOnly(t *testing.T) {
	for _, tc := range []struct {
		trigger sdd.SyncTrigger
		titled  bool
	}{
		{trigger: sdd.SyncOnCall},
		{trigger: sdd.SyncOnSessionStart, titled: true},
		{trigger: sdd.SyncOnTimer, titled: true},
	} {
		t.Run(string(tc.trigger), func(t *testing.T) {
			repo, _ := syncedRepository(t, "manual")
			repo.write(".sdd/config.local.yaml", "sync:\n  mode: manual\n  cooldown: 1h\n")
			repo.write(".sdd/tmp/last-titles", "")
			path, content := untitledEntry("20261101-100000-s-tac-cdn")
			repo.write(path, content)
			repo.git("add", path)
			repo.git("commit", "-m", "sdd: signal tactical gap")

			err := repo.targets().BackfillTitles(t.Context(), "main", tc.trigger, titleOnlyRunner{})
			if err != nil {
				t.Fatal(err)
			}
			titled := repo.git("log", "-1", "--format=%s") == "sdd: summarize 1 entries"
			if titled != tc.titled {
				t.Error("a call inside the cooldown should not backfill, a session start or timer round always should")
			}
		})
	}
}

// A capture writes and commits under the graph lock; the backfill's write
// waits for it, so neither overwrites the other.
func TestBackfillWaitsForTheGraphLock(t *testing.T) {
	repo, _ := syncedRepository(t, "manual")
	path, content := untitledEntry("20261101-100000-s-tac-lck")
	repo.write(path, content)
	repo.git("add", path)
	repo.git("commit", "-m", "sdd: signal tactical gap")
	held, err := syncstate.LockGraph(filepath.Join(repo.root, ".sdd", "graph"))
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- repo.targets().BackfillTitles(context.WithoutCancel(t.Context()), "main", sdd.SyncOnSessionStart, titleOnlyRunner{})
	}()
	select {
	case err := <-done:
		syncstate.Unlock(held)
		t.Fatalf("the backfill should wait while another writer holds the graph lock, returned %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	if strings.Contains(repo.git("show", "HEAD:"+path), "title:") {
		t.Error("nothing should be committed while the graph lock is held")
	}
	syncstate.Unlock(held)
	err = <-done
	if err != nil {
		t.Fatal(err)
	}
	if subject := repo.git("log", "-1", "--format=%s"); subject != "sdd: summarize 1 entries" {
		t.Errorf("the backfill should commit once the lock is free, got %q", subject)
	}
}
