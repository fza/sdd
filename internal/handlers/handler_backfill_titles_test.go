package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/handlers/synctest"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

// backfillEntryID is the ID of the n-th untitled entry; a higher n is newer.
func backfillEntryID(n int) string {
	return fmt.Sprintf("20261101-%06d-s-tac-b%02d", 100000+n, n)
}

// writeUntitled writes an untitled entry whose stored summary names its ID,
// so a title-only call can be told apart by its prompt.
func writeUntitled(t *testing.T, graphDir string, n int) string {
	t.Helper()
	id := backfillEntryID(n)
	writeEntryFile(t, graphDir, id, fmt.Sprintf("---\ntype: signal\nlayer: tactical\nkind: gap\nsummary: Summary of %s.\n---\n\nBody of %s.\n", id, id))
	return id
}

var promptedID = regexp.MustCompile(`20261101-\d{6}-s-tac-b\d{2}`)

// titleRunner answers each description call with a title naming the entry
// it was asked about, failing the entries listed in fail and running before,
// when set, ahead of every answer.
type titleRunner struct {
	mu      sync.Mutex
	calls   map[string]int
	fail    map[string]bool
	failAll bool
	before  func(id string)
	title   string
}

func (r *titleRunner) Run(_ context.Context, request llm.Request) (llm.Result, error) {
	id := promptedID.FindString(request.UserPrompt)
	r.mu.Lock()
	if r.calls == nil {
		r.calls = map[string]int{}
	}
	r.calls[id]++
	failed := r.failAll || r.fail[id]
	r.mu.Unlock()
	if failed {
		return llm.Result{}, errors.New("title route unavailable")
	}
	if r.before != nil {
		r.before(id)
	}
	title := r.title
	if title == "" {
		title = "title of " + id
	}
	answer := map[string]string{"title": title}
	if request.Purpose == llm.PurposeSummarize {
		answer["summary"] = "Generated summary."
	}
	data, err := json.Marshal(answer)
	if err != nil {
		return llm.Result{}, err
	}
	return llm.Result{Text: string(data)}, nil
}

func (r *titleRunner) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	total := 0
	for _, n := range r.calls {
		total += n
	}
	return total
}

type backfillFixture struct {
	graphDir  string
	sddDir    string
	runner    *titleRunner
	committer *synctest.Committer
	locks     *synctest.CheckoutLocks
	record    *synctest.Record
}

func newBackfillFixture(t *testing.T) *backfillFixture {
	t.Helper()
	return &backfillFixture{
		graphDir: t.TempDir(), sddDir: t.TempDir(), runner: &titleRunner{},
		committer: &synctest.Committer{}, locks: &synctest.CheckoutLocks{}, record: &synctest.Record{},
	}
}

func (f *backfillFixture) handler(runner llm.Runner) *handlers.Handler {
	return handlers.New(handlers.Options{
		GraphDir: f.graphDir, Reader: &graphReader{}, LLMRunner: runner, Committer: f.committer,
		GraphLock: f.locks, TitlesLock: f.locks, SyncRecord: f.record,
	})
}

func (f *backfillFixture) backfill(t *testing.T) {
	t.Helper()
	err := f.handler(f.runner).BackfillTitles(context.Background(), &command.BackfillTitlesCmd{SDDDir: f.sddDir})
	if err != nil {
		t.Fatalf("BackfillTitles: %v", err)
	}
}

func (f *backfillFixture) title(t *testing.T, id string) string {
	t.Helper()
	entry, err := model.ParseEntry(id+".md", readEntryFile(t, f.graphDir, id))
	if err != nil {
		t.Fatal(err)
	}
	return entry.Title
}

func TestBackfillTitlesTitlesTheNewestTenInOneCommit(t *testing.T) {
	f := newBackfillFixture(t)
	for n := 1; n <= 12; n++ {
		writeUntitled(t, f.graphDir, n)
	}
	writeEntryFile(t, f.graphDir, "20261102-100000-d-tac-ttl", "---\ntype: decision\nlayer: tactical\nkind: directive\ntitle: Kept title\nsummary: Titled already.\n---\n\nBody.\n")

	f.backfill(t)

	for n := 1; n <= 12; n++ {
		id := backfillEntryID(n)
		titled := f.title(t, id) != ""
		if titled != (n > 2) {
			t.Errorf("only the ten newest untitled entries should be titled; %s titled: %v", id, titled)
		}
	}
	if f.title(t, "20261102-100000-d-tac-ttl") != "Kept title" {
		t.Error("a titled entry should be left alone")
	}
	if len(f.committer.Messages) != 1 || f.committer.Messages[0] != "sdd: summarize 10 entries" || len(f.committer.Paths[0]) != 10 {
		t.Errorf("one run should commit every title in one summarize commit, got %v", f.committer.Messages)
	}
	if !strings.Contains(readEntryFile(t, f.graphDir, backfillEntryID(12)), "summary: Summary of "+backfillEntryID(12)) {
		t.Error("a stored summary should survive its entry's title")
	}
	if f.record.Current != nil {
		t.Errorf("a backfill that succeeds should record nothing, got %+v", f.record.Current)
	}
}

func TestBackfillTitlesGivesAnUnsummarizedEntryBothFromOneCall(t *testing.T) {
	f := newBackfillFixture(t)
	id := backfillEntryID(1)
	writeEntryFile(t, f.graphDir, id, "---\ntype: signal\nlayer: tactical\nkind: gap\n---\n\nBody of "+id+".\n")

	f.backfill(t)

	entry, err := model.ParseEntry(id+".md", readEntryFile(t, f.graphDir, id))
	if err != nil {
		t.Fatal(err)
	}
	if entry.Title == "" || entry.Summary != "Generated summary." || f.runner.callCount() != 1 {
		t.Errorf("an entry without a summary should get title and summary from one call, got %+v after %d calls", entry, f.runner.callCount())
	}
}

func TestBackfillTitlesKeepsATitleWrittenMeanwhile(t *testing.T) {
	f := newBackfillFixture(t)
	raced := writeUntitled(t, f.graphDir, 1)
	other := writeUntitled(t, f.graphDir, 2)
	f.runner.before = func(id string) {
		if id != raced {
			return
		}
		writeEntryFile(t, f.graphDir, raced, "---\ntype: signal\nlayer: tactical\nkind: gap\ntitle: Written meanwhile\nsummary: Summary of "+raced+".\n---\n\nBody of "+raced+".\n")
	}

	f.backfill(t)

	if got := f.title(t, raced); got != "Written meanwhile" {
		t.Errorf("an entry titled between the scan and the write should keep its title, got %q", got)
	}
	if f.title(t, other) == "" {
		t.Error("the other entry should still be titled")
	}
	if len(f.committer.Paths) != 1 || len(f.committer.Paths[0]) != 1 {
		t.Errorf("only the entry the backfill wrote should be committed, got %v", f.committer.Paths)
	}
	if f.locks.GraphTaken < 3 {
		t.Errorf("each write and the commit should take the graph lock, taken %d times", f.locks.GraphTaken)
	}
}

func TestBackfillTitlesCommitsTheOthersPastAFailedCall(t *testing.T) {
	f := newBackfillFixture(t)
	failing := writeUntitled(t, f.graphDir, 1)
	titled := writeUntitled(t, f.graphDir, 2)
	f.runner.fail = map[string]bool{failing: true}

	f.backfill(t)

	if f.title(t, titled) == "" || f.title(t, failing) != "" {
		t.Error("one failed call should leave the other title written and the failed entry untitled")
	}
	if len(f.committer.Messages) != 1 || f.committer.Messages[0] != "sdd: summarize 1 entries" {
		t.Errorf("the title written should be committed despite the failure, got %v", f.committer.Messages)
	}
	if f.record.Current == nil || !f.record.Current.TitlesFailed() {
		t.Errorf("the failure should be recorded for the sync notice, got %+v", f.record.Current)
	}
}

func TestBackfillTitlesFailureReachesTheNoticeAndIsRetried(t *testing.T) {
	f := newBackfillFixture(t)
	id := writeUntitled(t, f.graphDir, 1)
	f.runner.failAll = true

	f.backfill(t)

	if f.title(t, id) != "" || len(f.committer.Messages) != 0 {
		t.Error("a failed backfill should leave the entry untitled and commit nothing")
	}
	for _, titlesOnly := range []bool{false, true} {
		var notice string
		err := f.handler(nil).ClaimSyncNotice(context.Background(), &command.ClaimSyncNoticeCmd{
			Session: "s_1", TitlesOnly: titlesOnly, OnNotice: func(text string) { notice = text },
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(notice, "titling untitled entries failed") || strings.Contains(notice, "title route unavailable") {
			t.Errorf("the next response should carry the failure as the sync notice, in sdd's own words, got %q", notice)
		}
	}

	f.runner.failAll = false
	f.backfill(t)

	if f.runner.calls[id] != 2 || f.title(t, id) == "" {
		t.Error("the next trigger should retry the untitled entry")
	}
	if f.record.Current != nil {
		t.Errorf("a backfill that succeeds should clear the failure, got %+v", f.record.Current)
	}
}

func TestBackfillTitlesWithoutAnLLMRecordsTheFailure(t *testing.T) {
	f := newBackfillFixture(t)
	writeUntitled(t, f.graphDir, 1)
	err := f.handler(nil).BackfillTitles(context.Background(), &command.BackfillTitlesCmd{SDDDir: f.sddDir})
	if err != nil {
		t.Fatalf("a missing LLM should be recorded, not fail the trigger: %v", err)
	}
	if f.record.Current == nil || f.record.Current.Detail != "no LLM is configured" {
		t.Errorf("the missing LLM should be recorded, got %+v", f.record.Current)
	}
}

func TestBackfillTitlesLeavesABlockedSyncRecorded(t *testing.T) {
	f := newBackfillFixture(t)
	writeUntitled(t, f.graphDir, 1)
	f.record.Current = &model.SyncBlocked{Kind: model.SyncBlockedNoUpstream}
	f.runner.failAll = true

	f.backfill(t)

	if f.record.Current.Kind != model.SyncBlockedNoUpstream {
		t.Errorf("a blocked sync should stay recorded over a failed backfill, got %+v", f.record.Current)
	}
	var notice string
	err := f.handler(nil).ClaimSyncNotice(context.Background(), &command.ClaimSyncNoticeCmd{Session: "s_1", TitlesOnly: true, OnNotice: func(text string) { notice = text }})
	if err != nil {
		t.Fatal(err)
	}
	if notice != "" || f.record.Current.Owner != "" {
		t.Errorf("under manual sync a blocked sync should be neither served nor claimed, got %q", notice)
	}
}

func TestBackfillTitlesRunsOncePerCheckoutAndCooldown(t *testing.T) {
	for _, tc := range []struct {
		name     string
		held     bool
		cooldown bool
		wantRun  bool
	}{
		{name: "a backfill already running", held: true},
		{name: "a call inside the cooldown", cooldown: true},
		{name: "a trigger without cooldown", wantRun: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBackfillFixture(t)
			id := writeUntitled(t, f.graphDir, 1)
			f.locks.TitlesHeld = tc.held
			cmd := &command.BackfillTitlesCmd{SDDDir: f.sddDir}
			if tc.cooldown {
				f.backfill(t)
				writeUntitled(t, f.graphDir, 2)
				cmd.Cooldown = time.Hour
			}
			skipped := false
			cmd.OnSkipped = func() { skipped = true }
			err := f.handler(f.runner).BackfillTitles(context.Background(), cmd)
			if err != nil {
				t.Fatal(err)
			}
			ran := !skipped && f.title(t, backfillEntryID(1)) != "" && (!tc.cooldown || f.title(t, backfillEntryID(2)) != "")
			if ran != tc.wantRun {
				t.Errorf("the backfill should run only when no backfill runs and the cooldown passed; ran %v, skipped %v, %s titled %q", ran, skipped, id, f.title(t, id))
			}
		})
	}
}

func TestSyncRoundKeepsAFailedBackfillRecorded(t *testing.T) {
	record := &synctest.Record{Current: &model.SyncBlocked{Kind: model.SyncBlockedTitles, Detail: "a title call or its commit failed"}}
	repo := &synctest.Repo{Upstream: "origin/main"}
	h := handlers.New(handlers.Options{
		GraphDir: t.TempDir(), SyncMode: model.SyncModeAuto, GraphFetcher: repo, GraphPuller: repo, Pusher: repo,
		SyncLock: &synctest.Lock{}, SyncRecord: record,
	})
	err := h.SyncRound(context.Background(), &command.SyncRoundCmd{})
	if err != nil {
		t.Fatal(err)
	}
	if record.Current == nil || !record.Current.TitlesFailed() {
		t.Errorf("a sync round that succeeds should leave a failed backfill for the backfill to clear, got %+v", record.Current)
	}
}

// Two clones backfilling the same entry write two titles; the entry merge
// driver settles them without a conflict, the same in either direction.
func TestBackfilledTitlesFromTwoClonesMerge(t *testing.T) {
	var versions []string
	var base string
	for _, title := range []string{"Zebra crossing title", "Aardvark title"} {
		f := newBackfillFixture(t)
		id := writeUntitled(t, f.graphDir, 1)
		base = readEntryFile(t, f.graphDir, id)
		f.runner.title = title
		f.backfill(t)
		versions = append(versions, readEntryFile(t, f.graphDir, id))
	}

	merge := func(ours, theirs string) string {
		dir := t.TempDir()
		for name, content := range map[string]string{"base": base, "ours": ours, "theirs": theirs} {
			err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
			if err != nil {
				t.Fatal(err)
			}
		}
		err := handlers.New(handlers.Options{FileMerger: git.CLI{}}).MergeEntry(context.Background(), &command.MergeEntryCmd{
			BasePath: filepath.Join(dir, "base"), OursPath: filepath.Join(dir, "ours"), TheirsPath: filepath.Join(dir, "theirs"),
		})
		if err != nil {
			t.Fatalf("two backfilled titles should merge without a conflict: %v", err)
		}
		merged, err := os.ReadFile(filepath.Join(dir, "ours"))
		if err != nil {
			t.Fatal(err)
		}
		return string(merged)
	}
	forward, backward := merge(versions[0], versions[1]), merge(versions[1], versions[0])
	if forward != backward || forward != versions[1] {
		t.Errorf("both clones should converge on the title sorting first:\n%s\nagainst\n%s", forward, backward)
	}
}
