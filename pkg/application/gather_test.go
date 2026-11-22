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
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
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
	// proofs are the proof records the hiding rules read; nil runs without.
	proofs    sdd.ProofStore
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
		Project: sdd.ProjectRef{ID: gatherProject}, Graph: graph, LLM: f.llm, Grounding: policy, Relevance: f.relevance, Proofs: f.proofs, Prefilter: f.prefilter, Ranking: f.ranking,
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

// Without an embedding model a check runs the keyword searches and the
// topic listings on a graph of any size and states that no search by
// meaning ran.
func TestGather_SearchesWithoutAnEmbeddingModel(t *testing.T) {
	f := newGatherFixture(t)
	f.seed(t)
	for i := range 120 {
		f.write(t, fmt.Sprintf("20260102-1%05d-s-tac-p%03d", i, i), "gap", fmt.Sprintf("Padding entry %d.", i), "")
	}
	f.withoutEmbedder = true
	application := f.app(t, sdd.GroundingPolicy{}, nil)

	result, err := gather(t, application, sdd.GatherRequest{Draft: "A draft about alpha.", Kind: "gap", Topics: []string{"cli/ux"}, Query: "alpha matters", Terms: []string{"beta"}})
	if err != nil {
		t.Fatalf("a check without an embedding model should run, got %v", err)
	}
	for _, want := range []string{`keyword "beta"`, "listing topic(cli/ux)", gatherBeta, gatherTopicOnly, presenters.NoMeaningSearchNote} {
		if !strings.Contains(result.Text, want) {
			t.Errorf("serve lacks %q:\n%s", want, result.Text)
		}
	}
	if strings.Contains(result.Text, "semantic") {
		t.Errorf("no search by meaning should run without an embedding model:\n%s", result.Text)
	}
}

func TestGather_ChecksTheEmbedderBeforeSearching(t *testing.T) {
	f := newGatherFixture(t)
	f.seed(t)

	t.Run("unreachable", func(t *testing.T) {
		down := embed.EmbedderFunc{Space: "down/v1", Run: func(context.Context, embed.Request) (embed.Result, error) {
			return embed.Result{}, errors.New("connection refused")
		}}
		_, err := gather(t, f.app(t, sdd.GroundingPolicy{}, down), sdd.GatherRequest{Draft: "A draft about alpha."})
		if err == nil || !strings.Contains(err.Error(), "does not answer") {
			t.Fatalf("an unreachable embedder should fail, got %v", err)
		}
	})
	t.Run("unreachable model named", func(t *testing.T) {
		down := embed.EmbedderFunc{Space: "down/v1", Run: func(context.Context, embed.Request) (embed.Result, error) {
			identity := pkgllm.Identity{Provider: "ollama", Model: "nomic-embed-text", Host: "localhost:11434"}
			return embed.Result{}, &pkgllm.Error{Identity: identity, Err: errors.New("connection refused")}
		}}
		_, err := gather(t, f.app(t, sdd.GroundingPolicy{}, down), sdd.GatherRequest{Draft: "A draft about alpha."})
		if err == nil || !strings.Contains(err.Error(), "ollama/nomic-embed-text at localhost:11434 does not answer") {
			t.Fatalf("an unreachable embedder should fail naming the model and its host, got %v", err)
		}
	})
	t.Run("stalled", func(t *testing.T) {
		var limit time.Duration
		stalled := embed.EmbedderFunc{Space: "stalled/v1", Run: func(ctx context.Context, _ embed.Request) (embed.Result, error) {
			deadline, ok := ctx.Deadline()
			if ok {
				limit = time.Until(deadline)
			}
			identity := pkgllm.Identity{Provider: "ollama", Model: "qwen3-embedding:8b", Host: "gpu.local"}
			return embed.Result{}, &pkgllm.Error{Identity: identity, Err: fmt.Errorf("ollama embed request: %w", context.DeadlineExceeded)}
		}}
		_, err := gather(t, f.app(t, sdd.GroundingPolicy{}, stalled), sdd.GatherRequest{Draft: "A draft about alpha."})
		if limit <= 0 || limit > 30*time.Second {
			t.Fatalf("probe limit %s: the probe should run under a short limit of its own", limit)
		}
		if err == nil || !strings.Contains(err.Error(), "ollama/qwen3-embedding:8b at gpu.local did not answer within 30s") {
			t.Fatalf("a stalled embedder should fail naming the model, its host and the limit, got %v", err)
		}
	})
}

// WarmEmbedder embeds one probe, so a served project's model loads before
// its first gather; a project without an embedding model warms nothing.
func TestWarmEmbedder_EmbedsTheProbeOnce(t *testing.T) {
	f := newGatherFixture(t)
	emb := &countingEmbeddings{}
	err := f.app(t, sdd.GroundingPolicy{}, emb).WarmEmbedder(t.Context(), sdd.RequestIdentity{Subject: "tester"}, gatherProject)
	if err != nil || emb.queryEmbeds != 1 || emb.docEmbeds != 0 {
		t.Fatalf("%d query and %d document embeds, error %v: the warm-up should embed one probe", emb.queryEmbeds, emb.docEmbeds, err)
	}
	err = f.app(t, sdd.GroundingPolicy{}, nil).WarmEmbedder(t.Context(), sdd.RequestIdentity{Subject: "tester"}, gatherProject)
	if err != nil {
		t.Fatalf("a project without an embedding model should warm nothing, got %v", err)
	}
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
	if !strings.Contains(result.Text, `keyword "alpha" 2`) {
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
	if !slices.Equal(result.FullIDs, ids[:15]) {
		t.Errorf("full %v: one call should open every entry it names, with no cap", result.FullIDs)
	}
}

// A graph check serves no entry in full: it serves lines with their marks,
// the counts and the instruction, word for word.
func TestGather_ServesLinesAndTheInstruction(t *testing.T) {
	f := newGatherFixture(t)
	f.seed(t)
	application := f.app(t, sdd.GroundingPolicy{}, nil)
	result, err := gather(t, application, sdd.GatherRequest{Draft: "A draft about alpha."})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.FullIDs) != 0 || strings.Contains(result.Text, "id: ") {
		t.Fatalf("full %v: a check should serve no entry in full:\n%s", result.FullIDs, result.Text)
	}
	if !strings.Contains(result.Text, presenters.GatherInstruction) || presenters.GatherInstruction != "Open every entry you would reference and every entry that could contradict the draft. Then open those that look like bearing on the topic." {
		t.Fatalf("the check should carry the instruction word for word:\n%s", result.Text)
	}
	if !strings.Contains(result.Text, "Whenever a change gives the draft a fundamentally different shape, run a new graph check.") {
		t.Fatalf("the check should state that a draft of a fundamentally different shape needs a new check:\n%s", result.Text)
	}
	n := len(result.SummaryIDs)
	if !strings.HasPrefix(result.Text, fmt.Sprintf("# graph check   %d candidates · judged 0 of %d · %d shown · 0 set aside\n", n, n, n)) || n < model.FallbackMarks {
		t.Fatalf("the header should count the candidates found, judged, shown and set aside:\n%s", result.Text)
	}
}

// Opening entries by ID inside a begun reading runs no search, listing or
// judge call; outside one, a call naming only IDs opens them alone.
func TestGather_OpeningByIDRunsNothingElse(t *testing.T) {
	f := newGatherFixture(t)
	f.seed(t)
	scorer := &scriptedScorer{}
	f.prefilter = sdd.Prefilter{Scorer: scorer, Judge: gatherJudge}
	emb := &countingEmbeddings{}
	application := f.app(t, sdd.GroundingPolicy{Prefilter: true, PrefilterFrom: 1}, emb)
	result, err := gather(t, application, sdd.GatherRequest{IDs: []string{gatherAlpha, gatherBeta}})
	if err != nil {
		t.Fatal(err)
	}
	if f.llm.count() != 0 || emb.queryEmbeds != 0 || scorer.calls() != 0 {
		t.Fatalf("%d LLM calls, %d query embeds, %d judge calls: opening by ID should run nothing else", f.llm.count(), emb.queryEmbeds, scorer.calls())
	}
	if !slices.Equal(result.FullIDs, []string{gatherAlpha, gatherBeta}) || len(result.SummaryIDs) != 0 {
		t.Fatalf("full %v, lines %v: the call should open both entries and list nothing else", result.FullIDs, result.SummaryIDs)
	}
}

func TestGather_HeldEntryPrintsAsPointer(t *testing.T) {
	f := newGatherFixture(t)
	f.seed(t)
	application := f.app(t, sdd.GroundingPolicy{}, nil)

	result, err := gather(t, application, sdd.GatherRequest{Draft: "A draft about alpha and beta.", Held: func(id string) bool { return id == gatherBeta }})
	if err != nil {
		t.Fatal(err)
	}
	pointer := gatherBeta + " — The beta entry is about beta matters. (served in full above or in the result before)"
	if !strings.Contains(result.Text, pointer) {
		t.Errorf("a held entry should print as id, brief and pointer:\n%s", result.Text)
	}
	if !strings.Contains(result.Text, "show with its id") {
		t.Errorf("the serve should name the way back:\n%s", result.Text)
	}
	if strings.Contains(result.Text, gatherAlpha+" — The alpha entry is about alpha matters. (served in full") {
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
	if setAsideCount(result.Text, model.SetAsideJudged) != 1 {
		t.Errorf("a stored dismissal should set its pair aside without a new judgment, counted without its ID:\n%s", result.Text)
	}
	if line := lineOf(result.Text, gatherTopicOnly); !strings.HasPrefix(line, "!") {
		t.Errorf("a stored reference should mark its pair as likely bearing:\n%s", result.Text)
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
