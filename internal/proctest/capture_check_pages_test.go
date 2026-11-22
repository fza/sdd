// A capture's graph check too large for one result serves its first page and
// builds each later page when asked for, from the scores that arrived by
// then; only the pages served count as read (20261121-223938-d-tac-rqi).
package proctest_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	"github.com/networkteam/sdd/internal/relevance"
	sdd "github.com/networkteam/sdd/pkg/application"
	pkgrelevance "github.com/networkteam/sdd/pkg/llm/relevance"
	"github.com/networkteam/sdd/pkg/local"
	"github.com/networkteam/sdd/test"
)

// pagedEntries are 90 gaps under the draft's topic, each line long enough
// that a check of them runs past one page of the smallest result bound.
func pagedEntries() []*model.Entry {
	var entries []*model.Entry
	for i := range 90 {
		text := fmt.Sprintf("Paged gap %02d: a candidate whose summary runs long enough that ninety of them fill more than one page of a graph check, with a few more words on what it observes, so that every line it prints takes its share of the page.", i)
		entries = append(entries, &model.Entry{
			ID: pagedID(i), Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical,
			Summary: text, Content: text, Topics: proctest.MustTopics("cli/ux"),
		})
	}
	return entries
}

func pagedID(i int) string { return fmt.Sprintf("20260502-10%02d%02d-s-tac-p%02d", i/60, i%60, i) }

var pagedLine = regexp.MustCompile(`20260502-10\d{4}-s-tac-p\d{2}`)

func pagedWorld(t *testing.T, connID string, options ...proctest.Option) (*proctest.World, *proctest.Session, string) {
	t.Helper()
	world, session := newCaptureWorld(t, connID, append(options, proctest.WithEntries(pagedEntries()...))...)
	world.GatherTopics = nil
	instance := session.Start(t, "capture", nil).Instance
	session.Report(t, instance, map[string]any{"body": "A tactical gap: the fixture observes paging.", "entryKind": "gap", "topics": []any{"cli/ux"}})
	return world, session, instance
}

func pagedGather(t *testing.T, world *proctest.World, session *proctest.Session, instance string, page int) sdd.GatherResult {
	t.Helper()
	ctx := sdd.WithResultBound(t.Context(), int(model.DefaultResultBound))
	result, err := session.WF.Gather(ctx, world.Identity, sdd.WorkflowGatherRequest{
		Instance: instance, Query: world.GatherQuery, Terms: world.GatherTerms, Paged: true, Page: page, SyncMode: sdd.SearchSyncLocal,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// Across the pages of one check, each within the bound, no entry appears
// twice and none is lost.
func TestCaptureCheckPages_EveryEntryOnceAcrossThePages(t *testing.T) {
	world, session, instance := pagedWorld(t, "check-pages", proctest.WithGroundingEnforced())

	seen := map[string]int{}
	var last string
	for page := 1; page == 1 || strings.Contains(last, fmt.Sprintf("and page %d;", page)); page++ {
		last = pagedGather(t, world, session, instance, page).Text
		if model.ServedChars(last) > model.DefaultResultBound.Read() {
			t.Fatalf("page %d runs past the bound:\n%s", page, last)
		}
		if page == 1 && !strings.Contains(last, "and page 2;") {
			t.Fatalf("the check should hold lines for page 2:\n%s", last)
		}
		for _, id := range pagedLine.FindAllString(last, -1) {
			seen[id]++
		}
	}
	if !strings.Contains(last, "the last page of this graph check") {
		t.Fatalf("the check should end on its last page:\n%s", last)
	}
	for i := range 90 {
		if seen[pagedID(i)] != 1 {
			t.Fatalf("%s appears %d times across the pages: every entry should appear once", pagedID(i), seen[pagedID(i)])
		}
	}
}

// A read is credited for the pages served: an entry the first page showed
// counts as read at publish, one only a page never fetched holds does not.
func TestCaptureCheckPages_OnlyServedPagesCountAsRead(t *testing.T) {
	store := local.NewMemoryRelevanceStore()
	world, session, instance := pagedWorld(t, "check-pages-credit", proctest.WithGroundingEnforced(), proctest.WithRelevance(store))

	first := pagedGather(t, world, session, instance, 1)
	shown := pagedLine.FindAllString(first.Text, -1)
	var unfetched string
	for i := range 90 {
		if !strings.Contains(first.Text, pagedID(i)) {
			unfetched = pagedID(i)
			break
		}
	}
	if len(shown) == 0 || unfetched == "" {
		t.Fatalf("the first page should show some paged gaps and hold others:\n%s", first.Text)
	}
	serve := session.Report(t, instance, answerMarked(first.Text, captureDraft()))
	proctest.RequireStep(t, serve, "playback")
	entry := confirmCapture(t, world, session, instance)

	segments, err := store.Segments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := relevance.Fold(segments)
	if err != nil {
		t.Fatal(err)
	}
	if _, read := ledger.Reader(model.NewRelevancePair(entry.ID, shown[len(shown)-1])); !read {
		t.Fatalf("%s, shown on the served page, should count as read", shown[len(shown)-1])
	}
	if verdict, read := ledger.Reader(model.NewRelevancePair(entry.ID, unfetched)); read {
		t.Fatalf("%s, held for a page never fetched, should not count as read: %+v", unfetched, verdict)
	}
}

// A score that arrives after the first page reaches the page built next: a
// line it makes likely bearing comes marked under its own heading.
func TestCaptureCheckPages_ALateScoreMarksItsLineOnTheNextPage(t *testing.T) {
	release := make(chan struct{})
	scorer := &test.ScriptedScorer{Default: 0.99, Script: func(ctx context.Context, _ pkgrelevance.Request) error {
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	world, session, instance := pagedWorld(t, "check-pages-late",
		proctest.WithGrounding(sdd.GroundingPolicy{Enforce: true, Prefilter: true, PrefilterFrom: 1, JudgeTimeout: 300 * time.Millisecond, JudgeMinimum: 1}),
		proctest.WithRelevance(laneLedger(t, true)), proctest.WithProofs(provenThreshold(t, laneJudge)),
		proctest.WithPrefilter(sdd.Prefilter{Scorer: scorer, Judge: laneJudge}))

	first := pagedGather(t, world, session, instance, 1)
	if !strings.Contains(first.Text, "and page 2;") || strings.Contains(first.Text, ".99") {
		t.Fatalf("the check should judge nothing in time and hold lines for page 2:\n%s", first.Text)
	}
	close(release)
	second := pagedGather(t, world, session, instance, 2)
	_, late, ok := strings.Cut(second.Text, "## likely bearing, scored since the last page (")
	if !ok || !strings.HasPrefix(strings.SplitN(late, "\n", 3)[1], "! .99") {
		t.Fatalf("a late score should mark its line under its own heading:\n%s", second.Text)
	}
}
