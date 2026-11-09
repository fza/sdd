package application_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/relevance"
	"github.com/networkteam/sdd/internal/segment"
	sdd "github.com/networkteam/sdd/pkg/application"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/embed"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

const gatherProject = sdd.ProjectID("gather")

// gatherScript answers the query writer and counts every LLM call.
type gatherScript struct {
	mu    sync.Mutex
	calls int
	query string
	terms []string
}

func (s *gatherScript) Run(_ context.Context, request pkgllm.Request) (pkgllm.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if request.Purpose != pkgllm.PurposeGatherQuery {
		return pkgllm.Result{}, fmt.Errorf("unscripted purpose %s", request.Purpose)
	}
	quoted := make([]string, len(s.terms))
	for i, term := range s.terms {
		quoted[i] = fmt.Sprintf("%q", term)
	}
	text := fmt.Sprintf(`{"query": %q, "terms": [%s]}`, s.query, strings.Join(quoted, ", "))
	return pkgllm.Result{Text: text, Identity: pkgllm.Identity{Provider: "test", Model: "test"}}, nil
}

func (s *gatherScript) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type gatherFixture struct {
	graphDir string
	llm      *gatherScript
	// relevance is the ledger the runtime reads; nil runs without one.
	relevance sdd.RelevanceStore
	prefilter sdd.Prefilter
	events    sdd.EventStore
	handEdits sdd.HandEditScanner
}

func newGatherFixture(t *testing.T) *gatherFixture {
	return &gatherFixture{graphDir: t.TempDir(), llm: &gatherScript{query: "alpha matters", terms: []string{"beta"}}}
}

func (f *gatherFixture) write(t *testing.T, id, kind, summary, extra string) {
	t.Helper()
	dir := filepath.Join(f.graphDir, id[:4], id[4:6])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	typ := "signal"
	if strings.Contains(id, "-d-") {
		typ = "decision"
	}
	content := "---\ntype: " + typ + "\nkind: " + kind + "\nlayer: tactical\nconfidence: high\n" + extra +
		"summary: " + summary + "\n---\n\n" + summary + "\n"
	if err := os.WriteFile(filepath.Join(dir, id[6:]+".md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *gatherFixture) app(t *testing.T, policy sdd.GroundingPolicy, embeddings embed.Embedder) *sdd.Application {
	t.Helper()
	graph, err := localadapter.NewFilesystemGraphStore(localadapter.FilesystemGraphStoreOptions{Project: gatherProject, GraphDir: f.graphDir})
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
	options := sdd.ProjectRuntimeOptions{
		Project: sdd.ProjectRef{ID: gatherProject}, Graph: graph, LLM: f.llm, Grounding: policy, Relevance: f.relevance, Prefilter: f.prefilter,
		Events: f.events, HandEdits: f.handEdits,
	}
	if embeddings != nil {
		options.Embedder, options.RuneCap = embeddings, testRuneCap
		options.SearchIndex = localadapter.NewPersistentSearchIndexStore(gatherProject, t.TempDir(), "gather/repo")
	}
	runtime, err := sdd.NewProjectRuntime(options)
	if err != nil {
		t.Fatal(err)
	}
	application, err := sdd.NewApplication(sdd.ApplicationOptions{Access: &runtimeAccessResolver{runtime: runtime}, Sessions: sessions, StagedBlobs: blobs})
	if err != nil {
		t.Fatal(err)
	}
	return application
}

func gather(t *testing.T, application *sdd.Application, request sdd.GatherRequest) (sdd.GatherResult, error) {
	t.Helper()
	request.SyncMode = sdd.SearchSyncAll
	return application.Gather(t.Context(), sdd.RequestIdentity{Subject: "tester"}, gatherProject, request)
}

const (
	gatherAlpha      = "20260101-100000-s-tac-aaa"
	gatherBeta       = "20260101-100001-s-tac-bbb"
	gatherOld        = "20260101-100002-d-tac-old"
	gatherSuccessor  = "20260101-100003-d-tac-new"
	gatherActor      = "20260101-100004-s-prc-act"
	gatherTopicOnly  = "20260101-100005-s-tac-top"
	gatherUnrelated  = "20260101-100006-s-tac-unr"
	gatherSearchSize = 1
)

func (f *gatherFixture) seed(t *testing.T) {
	f.write(t, gatherAlpha, "gap", "The alpha entry is about alpha matters.", "")
	f.write(t, gatherBeta, "gap", "The beta entry is about beta matters.", "")
	f.write(t, gatherOld, "directive", "An old alpha directive the successor replaces.", "intent: pending\n")
	f.write(t, gatherSuccessor, "directive", "The successor directive on alpha.", "intent: pending\nsupersedes:\n  - "+gatherOld+"\n")
	f.write(t, gatherActor, "actor", "An alpha actor record.", "canonical: Alpha Person\n")
	f.write(t, gatherTopicOnly, "gap", "A gap that only shares the topic.", "topics:\n  - cli/ux\n")
	f.write(t, gatherUnrelated, "gap", "A gap nobody searches for.", "")
}

func TestGather_BelowTheLimitServesEveryLiveBriefAndRunsNoSearch(t *testing.T) {
	f := newGatherFixture(t)
	f.seed(t)
	emb := &countingEmbeddings{}
	application := f.app(t, sdd.GroundingPolicy{BriefAllBelow: 10_000}, emb)

	result, err := gather(t, application, sdd.GatherRequest{Draft: "A draft about alpha."})
	if err != nil {
		t.Fatal(err)
	}
	if f.llm.count() != 0 || emb.queryEmbeds != 0 {
		t.Fatalf("all-briefs mode must run no query writer and no search: %d LLM calls, %d query embeds", f.llm.count(), emb.queryEmbeds)
	}
	if !strings.Contains(result.Text, "every live brief, no search") {
		t.Errorf("the header should name the all-briefs read:\n%s", result.Text)
	}
	for _, id := range []string{gatherAlpha, gatherBeta, gatherSuccessor, gatherTopicOnly, gatherUnrelated} {
		if !strings.Contains(result.Text, id) {
			t.Errorf("live entry %s missing from the serve", id)
		}
	}
	if strings.Contains(result.Text, gatherOld) {
		t.Errorf("a superseded entry must never be a candidate:\n%s", result.Text)
	}
	if strings.Contains(result.Text, gatherActor) {
		t.Errorf("an actor record must never be a candidate:\n%s", result.Text)
	}
}

func TestGather_SearchModeChecksTheEmbedderBeforeAnyLLMCall(t *testing.T) {
	f := newGatherFixture(t)
	f.seed(t)

	t.Run("missing", func(t *testing.T) {
		_, err := gather(t, f.app(t, sdd.GroundingPolicy{BriefAllBelow: gatherSearchSize}, nil), sdd.GatherRequest{Draft: "A draft about alpha."})
		if err == nil || !strings.Contains(err.Error(), "embedding") {
			t.Fatalf("a missing embedder should fail naming it, got %v", err)
		}
		if f.llm.count() != 0 {
			t.Fatalf("the query writer ran before the embedder check: %d calls", f.llm.count())
		}
	})
	t.Run("unreachable", func(t *testing.T) {
		down := embed.EmbedderFunc{Space: "down/v1", Run: func(context.Context, embed.Request) (embed.Result, error) {
			return embed.Result{}, errors.New("connection refused")
		}}
		_, err := gather(t, f.app(t, sdd.GroundingPolicy{BriefAllBelow: gatherSearchSize}, down), sdd.GatherRequest{Draft: "A draft about alpha."})
		if err == nil || !strings.Contains(err.Error(), "does not answer") {
			t.Fatalf("an unreachable embedder should fail, got %v", err)
		}
		if f.llm.count() != 0 {
			t.Fatalf("the query writer ran before the embedder check: %d calls", f.llm.count())
		}
	})
}

func TestGather_SearchModeRunsTheWrittenQueriesAndTopicListings(t *testing.T) {
	f := newGatherFixture(t)
	f.seed(t)
	application := f.app(t, sdd.GroundingPolicy{BriefAllBelow: gatherSearchSize}, &countingEmbeddings{})

	result, err := gather(t, application, sdd.GatherRequest{Draft: "A draft about alpha.", Kind: "gap", Topics: []string{"cli/ux"}})
	if err != nil {
		t.Fatal(err)
	}
	if f.llm.count() != 1 {
		t.Fatalf("the query writer should run once, ran %d times", f.llm.count())
	}
	for _, want := range []string{`semantic "alpha matters"`, `keyword "beta"`, "listing topic(cli/ux)", gatherAlpha, gatherBeta, gatherTopicOnly} {
		if !strings.Contains(result.Text, want) {
			t.Errorf("serve lacks %q:\n%s", want, result.Text)
		}
	}
	if strings.Contains(result.Text, gatherOld) {
		t.Errorf("a superseded entry must never be a candidate:\n%s", result.Text)
	}
}

func TestGather_CutsNoTextAndDropsNoListItem(t *testing.T) {
	f := newGatherFixture(t)
	long := strings.Repeat("A long brief sentence part that keeps on going ", 30) + "and ends here."
	var ids []string
	for i := range 60 {
		id := fmt.Sprintf("20260101-1%05d-s-tac-l%02d", i, i)
		ids = append(ids, id)
		f.write(t, id, "gap", fmt.Sprintf("Entry %d %s", i, long), "")
	}
	application := f.app(t, sdd.GroundingPolicy{BriefAllBelow: 10_000}, nil)

	result, err := gather(t, application, sdd.GatherRequest{IDs: ids[:15]})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if !strings.Contains(result.Text, id) {
			t.Errorf("entry %s dropped from the serve", id)
		}
	}
	if strings.Count(result.Text, "and ends here.") < 60 {
		t.Errorf("a brief or body was cut: %d whole texts in the serve", strings.Count(result.Text, "and ends here."))
	}
	if !strings.Contains(result.Text, "+3 opened entries past the full-serve cap of 12") {
		t.Errorf("the entries past the full-serve cap should be named with the call that opens them:\n%s", result.Text)
	}
}

func TestGather_HeldEntryPrintsAsPointer(t *testing.T) {
	f := newGatherFixture(t)
	f.seed(t)
	application := f.app(t, sdd.GroundingPolicy{BriefAllBelow: 10_000}, nil)

	result, err := gather(t, application, sdd.GatherRequest{Held: func(id string) bool { return id == gatherBeta }})
	if err != nil {
		t.Fatal(err)
	}
	pointer := gatherBeta + " — The beta entry is about beta matters. (served in full above)"
	if !strings.Contains(result.Text, pointer) {
		t.Errorf("a held entry should print as id, brief and pointer:\n%s", result.Text)
	}
	if !strings.Contains(result.Text, "gather with its ids, or resume_session") {
		t.Errorf("the serve should name the way back:\n%s", result.Text)
	}
	if strings.Contains(result.Text, gatherAlpha+" — The alpha entry is about alpha matters. (served in full above)") {
		t.Errorf("an entry not held printed as a pointer:\n%s", result.Text)
	}
}

func TestGather_StoredVerdictsAnswerRepeatPairs(t *testing.T) {
	f := newGatherFixture(t)
	f.seed(t)
	store := localadapter.NewMemoryRelevanceStore()
	err := store.Append(t.Context(), relevance.Encode(model.RelevanceBatch{Readers: []model.ReaderRecord{
		{Pair: model.NewRelevancePair(gatherAlpha, gatherBeta), Verdict: model.ReaderVerdict{Code: model.ReaderDismissed, Reason: 1}},
		{Pair: model.NewRelevancePair(gatherTopicOnly, gatherAlpha), Verdict: model.ReaderVerdict{Code: model.ReaderReferenced}},
	}}))
	if err != nil {
		t.Fatal(err)
	}
	f.relevance = store
	application := f.app(t, sdd.GroundingPolicy{BriefAllBelow: 10_000}, nil)

	result, err := gather(t, application, sdd.GatherRequest{Subject: gatherAlpha})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Text, model.SetAsideJudged+": 1: "+gatherBeta) {
		t.Errorf("a stored dismissal should set its pair aside without a new judgment:\n%s", result.Text)
	}
	if !slices.Contains(result.FullIDs, gatherTopicOnly) {
		t.Errorf("a stored reference should serve its pair in full: full %v", result.FullIDs)
	}
	if slices.Contains(result.SummaryIDs, gatherBeta) || slices.Contains(result.SummaryIDs, gatherAlpha) {
		t.Errorf("neither the subject nor a decided pair stays to judge: %v", result.SummaryIDs)
	}
	if !slices.Contains(result.SummaryIDs, gatherUnrelated) {
		t.Errorf("an undecided pair stays to judge: %v", result.SummaryIDs)
	}
}

func TestGather_CorruptLedgerSegmentFailsLoud(t *testing.T) {
	f := newGatherFixture(t)
	f.seed(t)
	store := localadapter.NewMemoryRelevanceStore()
	if err := store.Append(t.Context(), []byte("garbage")); err != nil {
		t.Fatal(err)
	}
	f.relevance = store
	application := f.app(t, sdd.GroundingPolicy{BriefAllBelow: 10_000}, nil)
	if _, err := gather(t, application, sdd.GatherRequest{Draft: "A draft."}); !errors.Is(err, segment.ErrCorrupt) {
		t.Fatalf("gather = %v: a corrupt ledger segment should fail the read", err)
	}
}
