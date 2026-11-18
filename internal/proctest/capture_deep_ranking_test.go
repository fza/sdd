package proctest_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	"github.com/networkteam/sdd/internal/relevance"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/embed"
	pkgrelevance "github.com/networkteam/sdd/pkg/llm/relevance"
	"github.com/networkteam/sdd/pkg/local"
	"github.com/networkteam/sdd/test"
)

const (
	hostJudge   = "test:host"
	gatherJudge = "test:gather"
	// kappaEntry is the one lane entry sharing the draft's word kappa, so
	// similarity to the draft ranks it first.
	kappaEntry = "20260604-120000-d-tac-kap"
)

func kappaEntries() []*model.Entry {
	return append(laneEntries(), &model.Entry{
		ID: kappaEntry, Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerTactical, Intent: model.IntentPending,
		Summary: "A kappa directive.", Content: "A kappa directive.", Topics: proctest.MustTopics("misc"),
	})
}

func kappaDraft() map[string]any {
	draft := captureDraft()
	draft["body"] = "A tactical gap: the fixture observes kappa."
	return draft
}

// kappaEmbedder places every text holding kappa on one axis and the rest on
// the other.
func kappaEmbedder() embed.Embedder {
	return embed.EmbedderFunc{Space: "test:kappa", Run: func(_ context.Context, request embed.Request) (embed.Result, error) {
		result := embed.Result{Identity: llm.Identity{Provider: "test", Model: "kappa"}}
		for _, text := range request.Texts {
			vector := []float32{0.05, 1}
			if strings.Contains(strings.ToLower(text), "kappa") {
				vector = []float32{1, 0.05}
			}
			result.Vectors = append(result.Vectors, vector)
		}
		return result, nil
	}}
}

// awaitScored waits until scorer was asked about any entry.
func awaitScored(t *testing.T, scorer *test.ScriptedScorer) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(scorer.Scored()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the ranking judge should start scoring once assemble passes, before the verifier's reading")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Under a Claude host the deep reading ranks with the host's judge, begun in
// the background once assemble passes, not with gather's prefilter judge;
// the reading's header names the judge and each pair is paid once.
func TestCaptureDeep_AClaudeHostRanksWithItsJudgeFromAssembleOn(t *testing.T) {
	host := &test.ScriptedScorer{Scores: map[string]float64{laneRelevant: 0.9}}
	gather := &test.ScriptedScorer{}
	world, session := newDeepWorld(t, "claude-code", sdd.VerifierPolicy{}, proctest.WithEntries(laneEntries()...),
		proctest.WithPrefilter(sdd.Prefilter{Scorer: gather, Judge: gatherJudge}),
		proctest.WithRanking(sdd.Ranking{ClaudeHost: sdd.Prefilter{Scorer: host, Judge: hostJudge}}))
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Gather(t, instance)
	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "playback")
	awaitScored(t, host)
	serve = session.Answer(t, instance, "playback", "confirm", nil, "write it")
	proctest.RequireStep(t, serve, "deepDispatch")

	reading := verifierReading(t, dispatchDeep(t, world, session, instance))
	if !strings.Contains(reading, "Ranking source: judge "+hostJudge+", ") {
		t.Fatalf("the reading's header should name the host's judge:\n%s", reading)
	}
	if !strings.Contains(reading, "ID: "+laneRelevant) {
		t.Fatalf("the entry the host's judge rates relevant should be served in full:\n%s", reading)
	}
	if len(gather.Scored()) != 0 {
		t.Fatalf("gather's judge scored %v: the Claude host's judge ranks the deep reading", gather.Scored())
	}
	scored := host.Scored()
	if slices.Contains(scored, captureRefID) {
		t.Fatal("a declared target should not be scored: its lane is settled")
	}
	slices.Sort(scored)
	if len(slices.Compact(slices.Clone(scored))) != len(scored) {
		t.Fatalf("scored %v: the reading should collect the background pass, not pay a pair twice", scored)
	}
}

// On a Claude host one judge reads both: gather's prefilter scores with the
// host's judge too, not the configured prefilter's.
func TestCaptureDeep_AClaudeHostGathersWithItsRankingJudge(t *testing.T) {
	host := &test.ScriptedScorer{Scores: map[string]float64{laneRelevant: 0.9}}
	gather := &test.ScriptedScorer{}
	world, session := newDeepWorld(t, "claude-code", sdd.VerifierPolicy{}, proctest.WithEntries(laneEntries()...),
		proctest.WithGrounding(sdd.GroundingPolicy{Enforce: true, Prefilter: true, PrefilterFrom: 1}),
		proctest.WithPrefilter(sdd.Prefilter{Scorer: gather, Judge: gatherJudge}),
		proctest.WithRanking(sdd.Ranking{ClaudeHost: sdd.Prefilter{Scorer: host, Judge: hostJudge}}))
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Gather(t, instance)
	proctest.RequireStep(t, session.Report(t, instance, captureDraft()), "playback")
	gathered := session.Gather(t, instance)
	if !strings.Contains(gathered.Text, "judge "+hostJudge+" scored ") {
		t.Fatalf("gather's prefilter should score with the Claude host's judge:\n%s", gathered.Text)
	}
	serve = session.Answer(t, instance, "playback", "confirm", nil, "write it")
	proctest.RequireStep(t, serve, "deepDispatch")
	reading := verifierReading(t, dispatchDeep(t, world, session, instance))
	if !strings.Contains(reading, "judge "+hostJudge) {
		t.Fatalf("the reading's header should name the host's judge:\n%s", reading)
	}
	if len(gather.Scored()) != 0 {
		t.Fatalf("gather's configured judge scored %v on a Claude host", gather.Scored())
	}
}

// Under any other host both readings rank with the prefilter judge, never
// with the Claude host's.
func TestCaptureDeep_AnotherHostRanksWithThePrefilterJudge(t *testing.T) {
	host := &test.ScriptedScorer{}
	gather := &test.ScriptedScorer{Scores: map[string]float64{laneRelevant: 0.9}}
	world, session := newDeepWorld(t, "zed-agent", sdd.VerifierPolicy{}, proctest.WithEntries(laneEntries()...),
		proctest.WithPrefilter(sdd.Prefilter{Scorer: gather, Judge: gatherJudge}),
		proctest.WithRanking(sdd.Ranking{ClaudeHost: sdd.Prefilter{Scorer: host, Judge: hostJudge}}))
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	reading := verifierReading(t, dispatchDeep(t, world, session, instance))

	if len(host.Scored()) != 0 {
		t.Fatalf("the Claude host's judge scored %v under another host", host.Scored())
	}
	if !strings.Contains(reading, "Ranking source: judge "+gatherJudge+", ") || !strings.Contains(reading, "ID: "+laneRelevant) {
		t.Fatalf("the prefilter judge should rank the reading:\n%s", reading)
	}
}

// Under any other host without a prefilter judge the reading ranks by
// similarity to the draft, read from the search index as a search filled
// it, and its header says so.
func TestCaptureDeep_AnotherHostRanksBySimilarityWithoutAJudge(t *testing.T) {
	host := &test.ScriptedScorer{}
	world, session := newDeepWorld(t, "zed-agent", sdd.VerifierPolicy{}, proctest.WithEntries(kappaEntries()...),
		proctest.WithEmbedder(kappaEmbedder()),
		proctest.WithRanking(sdd.Ranking{ClaudeHost: sdd.Prefilter{Scorer: host, Judge: hostJudge}}))
	if _, err := world.App.Search(t.Context(), world.Identity, "proctest", sdd.SearchRequest{SyncMode: sdd.SearchSyncLocal, Phrase: "kappa"}); err != nil {
		t.Fatal(err)
	}
	instance, serve := confirmedDeepCapture(t, session, kappaDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	reading := verifierReading(t, dispatchDeep(t, world, session, instance))

	if len(host.Scored()) != 0 {
		t.Fatalf("the Claude host's judge scored %v under another host", host.Scored())
	}
	if !strings.Contains(reading, "Ranking source: similarity to the draft, ") && !strings.Contains(reading, " by similarity to the draft") {
		t.Fatalf("the reading's header should name similarity as its ranking source:\n%s", reading)
	}
	kappa := strings.Index(reading, "\n"+kappaEntry+"  ")
	low := strings.Index(reading, "\n20260603-120000-d-tac-l00  ")
	if kappa < 0 || low < 0 || kappa > low {
		t.Fatalf("the entry most similar to the draft should be listed first:\n%s", reading)
	}
}

// stuckScorer never answers until the test ends, whatever its context says.
type stuckScorer struct {
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

func (s *stuckScorer) Direction() pkgrelevance.Direction { return pkgrelevance.Both }
func (s *stuckScorer) MaxBatch() int                     { return 0 }
func (s *stuckScorer) MaxInputTokens() int               { return 0 }
func (s *stuckScorer) MaxPairTokens() int                { return 0 }

func (s *stuckScorer) Score(context.Context, pkgrelevance.Request) (pkgrelevance.Result, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	<-s.release
	return pkgrelevance.Result{}, fmt.Errorf("released")
}

// A judge that never answers holds the reading no longer than the ranking
// time limit: the reading comes in listed order and says so.
func TestCaptureDeep_ARankingPastItsTimeLimitLeavesListedOrder(t *testing.T) {
	stuck := &stuckScorer{release: make(chan struct{})}
	t.Cleanup(func() { close(stuck.release) })
	world, session := newDeepWorld(t, "claude-code", sdd.VerifierPolicy{}, proctest.WithEntries(laneEntries()...),
		proctest.WithRanking(sdd.Ranking{ClaudeHost: sdd.Prefilter{Scorer: stuck, Judge: hostJudge}, Timeout: 300 * time.Millisecond}))
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")

	start := time.Now()
	reading := verifierReading(t, dispatchDeep(t, world, session, instance))
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("the reading took %s: a stuck judge should hold it no longer than the time limit", elapsed)
	}
	if !strings.Contains(reading, "Ranking source: listed order, ") {
		t.Fatalf("with no score and no embedder the header should name listed order:\n%s", reading)
	}
	stuck.mu.Lock()
	defer stuck.mu.Unlock()
	if stuck.calls == 0 {
		t.Fatal("the stuck judge should have been asked")
	}
}

// A judge whose ranking recall over its referenced labels is below the bar
// widens the reading: the top served in full doubles, and the header says
// why.
func TestCaptureDeep_AWeakJudgeWidensTheReading(t *testing.T) {
	readingWith := func(t *testing.T, recall uint8) string {
		store := local.NewMemoryRelevanceStore()
		var batch model.RelevanceBatch
		for i := range model.RankingRecallMinimum {
			pair := model.NewRelevancePair(fmt.Sprintf("20250101-%06d-s-tac-pos", i), "20250601-000000-d-tac-sub")
			batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderReferenced}})
			batch.Scores = append(batch.Scores, model.ScoreRecord{Pair: pair, Score: model.FilterScore{Judge: hostJudge, Score: recall}})
		}
		if err := store.Append(t.Context(), relevance.Encode(batch)); err != nil {
			t.Fatal(err)
		}
		host := &test.ScriptedScorer{Scores: map[string]float64{laneRelevant: 0.9}, Default: 0.1}
		world, session := newDeepWorld(t, "claude-code", sdd.VerifierPolicy{}, proctest.WithEntries(laneEntries()...), proctest.WithRelevance(store),
			proctest.WithRanking(sdd.Ranking{ClaudeHost: sdd.Prefilter{Scorer: host, Judge: hostJudge}}))
		instance, serve := confirmedDeepCapture(t, session, captureDraft())
		proctest.RequireStep(t, serve, "deepDispatch")
		return verifierReading(t, dispatchDeep(t, world, session, instance))
	}
	weak, sound := readingWith(t, 10), readingWith(t, 250)

	if !strings.Contains(weak, "ranking recall is below the bar, so the reading serves the top 10 in full") {
		t.Fatalf("the header should say the weak judge widened the reading:\n%s", weak)
	}
	if strings.Contains(sound, "below the bar") {
		t.Fatalf("a sound judge should not widen the reading:\n%s", sound)
	}
	blocks := func(reading string) int { return strings.Count(reading, "---\n\nID: ") }
	if got := blocks(weak) - blocks(sound); got != model.DeepTopRanked {
		t.Fatalf("the weak judge's reading serves %d more entries in full, want %d", got, model.DeepTopRanked)
	}
}
