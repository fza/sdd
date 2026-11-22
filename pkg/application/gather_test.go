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

// gatherScript counts every LLM call and answers none: a check makes no
// model call of the main llm block.
type gatherScript struct {
	mu    sync.Mutex
	calls int
}

func (s *gatherScript) Run(_ context.Context, request pkgllm.Request) (pkgllm.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return pkgllm.Result{}, fmt.Errorf("unscripted purpose %s", request.Purpose)
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
	ranking   sdd.Ranking
	events    sdd.EventStore
	handEdits sdd.HandEditScanner
	runs      sdd.CalibrationRuns
	// withoutEmbedder leaves the runtime without an embedding model where
	// app is passed none; else it gets countingEmbeddings.
	withoutEmbedder bool
	// unsearched marks the entries write writes as far from every draft, so
	// the search with a draft's text lists them last.
	unsearched bool
}

func newGatherFixture(t *testing.T) *gatherFixture {
	return &gatherFixture{graphDir: t.TempDir(), llm: &gatherScript{}}
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
	mark := fixtureMark
	if f.unsearched {
		mark = farMark
	}
	content := "---\ntype: " + typ + "\nkind: " + kind + "\nlayer: tactical\nconfidence: high\n" + extra +
		"summary: " + summary + "\n---\n\n" + summary + "\n\n" + mark + "\n"
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
		Project: sdd.ProjectRef{ID: gatherProject}, Graph: graph, LLM: f.llm, Grounding: policy, Relevance: f.relevance, Prefilter: f.prefilter, Ranking: f.ranking,
		Events: f.events, HandEdits: f.handEdits, CalibrationRuns: f.runs,
	}
	if embeddings == nil && !f.withoutEmbedder {
		embeddings = &fixtureEmbeddings{}
	}
	if embeddings != nil {
		options.Embedder, options.RuneCap = embeddings, testRuneCap
		options.SearchIndex = localadapter.NewMemorySearchIndexStore()
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
	gatherAlpha     = "20260101-100000-s-tac-aaa"
	gatherBeta      = "20260101-100001-s-tac-bbb"
	gatherOld       = "20260101-100002-d-tac-old"
	gatherSuccessor = "20260101-100003-d-tac-new"
	gatherActor     = "20260101-100004-s-prc-act"
	gatherTopicOnly = "20260101-100005-s-tac-top"
	gatherUnrelated = "20260101-100006-s-tac-unr"
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

// A check on a graph of any size runs the search with the draft's own text,
// the agent's searches and the topic listings, and no model call writes
// the searches.
func TestGather_RunsTheDraftSearchTheAgentsSearchesAndTheListings(t *testing.T) {
	f := newGatherFixture(t)
	f.seed(t)
	emb := &countingEmbeddings{}
	application := f.app(t, sdd.GroundingPolicy{}, emb)

	result, err := gather(t, application, sdd.GatherRequest{Draft: "A draft about alpha.", Kind: "gap", Topics: []string{"cli/ux"}, Query: "alpha matters", Terms: []string{"beta"}})
	if err != nil {
		t.Fatal(err)
	}
	if f.llm.count() != 0 {
		t.Fatalf("no model call should write a check's searches: %d calls", f.llm.count())
	}
	if emb.queryEmbeds < 2 {
		t.Errorf("%d query embeds: the draft's text and the agent's phrase should each be searched by meaning", emb.queryEmbeds)
	}
	for _, want := range []string{"semantic, the draft's own text", `semantic "alpha matters"`, `keyword "beta"`, "listing topic(cli/ux)", gatherAlpha, gatherBeta, gatherTopicOnly} {
		if !strings.Contains(result.Text, want) {
			t.Errorf("serve lacks %q:\n%s", want, result.Text)
		}
	}
	if strings.Contains(result.Text, "every live brief") {
		t.Errorf("no check should serve every live brief in place of searching:\n%s", result.Text)
	}
	for _, id := range []string{gatherOld, gatherActor} {
		if strings.Contains(result.Text, id) {
			t.Errorf("a superseded entry or an actor record must never be a candidate: %s\n%s", id, result.Text)
		}
	}
}

// Without searches of its own the check still searches with the draft's
// text, whatever the size of the graph.
func TestGather_SearchesWithTheDraftsTextAlone(t *testing.T) {
	f := newGatherFixture(t)
	f.seed(t)
	application := f.app(t, sdd.GroundingPolicy{}, nil)

	result, err := gather(t, application, sdd.GatherRequest{Draft: "A draft about alpha."})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Text, "semantic, the draft's own text") || strings.Contains(result.Text, "keyword ") {
		t.Errorf("the check should run the draft's search alone:\n%s", result.Text)
	}
}

func TestGather_ChecksTheEmbedderBeforeSearching(t *testing.T) {
	f := newGatherFixture(t)
	f.seed(t)

	t.Run("missing", func(t *testing.T) {
		f.withoutEmbedder = true
		defer func() { f.withoutEmbedder = false }()
		_, err := gather(t, f.app(t, sdd.GroundingPolicy{}, nil), sdd.GatherRequest{Draft: "A draft about alpha."})
		if err == nil || !strings.Contains(err.Error(), "embedding") {
			t.Fatalf("a missing embedder should fail naming it, got %v", err)
		}
	})
	t.Run("unreachable", func(t *testing.T) {
		down := embed.EmbedderFunc{Space: "down/v1", Run: func(context.Context, embed.Request) (embed.Result, error) {
			return embed.Result{}, errors.New("connection refused")
		}}
		_, err := gather(t, f.app(t, sdd.GroundingPolicy{}, down), sdd.GatherRequest{Draft: "A draft about alpha."})
		if err == nil || !strings.Contains(err.Error(), "does not answer") {
			t.Fatalf("an unreachable embedder should fail, got %v", err)
		}
	})
}

// A gather with neither a draft, a search nor ids has nothing to read.
func TestGather_NeedsADraftASearchOrIDs(t *testing.T) {
	f := newGatherFixture(t)
	f.seed(t)
	_, err := gather(t, f.app(t, sdd.GroundingPolicy{}, nil), sdd.GatherRequest{})
	if err == nil || !strings.Contains(err.Error(), "needs the draft") {
		t.Fatalf("a bare gather should be refused, got %v", err)
	}
}

// A superseded entry or an actor record matching a search never becomes a
// candidate, so it takes none of the search's slots.
func TestGather_SearchHitsCountOnlyCandidates(t *testing.T) {
	f := newGatherFixture(t)
	f.seed(t)
	application := f.app(t, sdd.GroundingPolicy{}, nil)

	result, err := gather(t, application, sdd.GatherRequest{Draft: "A draft about alpha.", Kind: "gap", Terms: []string{"alpha"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Text, `keyword "alpha": 2 entries`) {
		t.Fatalf("the alpha search should count the live alpha gap and the successor alone:\n%s", result.Text)
	}
}

func TestGather_CutsNoTextAndDropsNoListItem(t *testing.T) {
	f := newGatherFixture(t)
	long := strings.Repeat("A long brief sentence part that keeps on going ", 30) + "and ends here."
	var ids []string
	for i := range 60 {
		id := fmt.Sprintf("20260101-1%05d-s-tac-l%02d", i, i)
		ids = append(ids, id)
		f.write(t, id, "gap", fmt.Sprintf("Entry %d %s", i, long), "topics:\n  - testing/long\n")
	}
	f.withoutEmbedder = true
	application := f.app(t, sdd.GroundingPolicy{}, nil)

	result, err := gather(t, application, sdd.GatherRequest{IDs: ids[:15], Terms: []string{"ends here"}, Topics: []string{"testing/long"}})
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
	application := f.app(t, sdd.GroundingPolicy{}, nil)

	result, err := gather(t, application, sdd.GatherRequest{Draft: "A draft about alpha and beta.", Held: func(id string) (string, string, bool) { return "ab12", "", id == gatherBeta }})
	if err != nil {
		t.Fatal(err)
	}
	pointer := gatherBeta + " — The beta entry is about beta matters. (held as tag ab12)"
	if !strings.Contains(result.Text, pointer) {
		t.Errorf("a held entry should print as id, brief and pointer:\n%s", result.Text)
	}
	if !strings.Contains(result.Text, "its tag left out of held") {
		t.Errorf("the serve should name the way back:\n%s", result.Text)
	}
	if strings.Contains(result.Text, gatherAlpha+" — The alpha entry is about alpha matters. (held as") {
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
	application := f.app(t, sdd.GroundingPolicy{}, nil)

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
	application := f.app(t, sdd.GroundingPolicy{}, nil)
	if _, err := gather(t, application, sdd.GatherRequest{Draft: "A draft."}); !errors.Is(err, segment.ErrCorrupt) {
		t.Fatalf("gather = %v: a corrupt ledger segment should fail the read", err)
	}
}

// fixtureMark ends the body of every entry the gather fixture writes, farMark
// that of one it writes unsearched.
const (
	fixtureMark = "A gather fixture entry."
	farMark     = "A gather fixture entry far from every draft."
)

// fixtureEmbeddings embeds as keywordVec does, with one more axis on which
// every query and every entry the fixture wrote lies near and one it wrote
// unsearched lies opposite, so a search with a draft's text lists the
// fixture's entries before the base entries the binary ships, and the
// unsearched ones last.
type fixtureEmbeddings struct{}

func (fixtureEmbeddings) Embed(_ context.Context, request embed.Request) (embed.Result, error) {
	vectors := make([][]float32, len(request.Texts))
	for i, text := range request.Texts {
		mark := float32(0)
		switch {
		case request.Purpose == embed.PurposeQuery:
			mark = 1
		case strings.Contains(text, farMark):
			mark = -1
		case strings.Contains(text, fixtureMark):
			mark = 1
		}
		vectors[i] = append(keywordVec(text), mark)
	}
	return embed.Result{Vectors: vectors}, nil
}

func (fixtureEmbeddings) Fingerprint() string { return "fixture/v1" }
