package proctest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/llm/claude"
	"github.com/networkteam/sdd/internal/llmops"
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
	if !strings.HasPrefix(readingLine(reading, laneRelevant), "! .90") {
		t.Fatalf("the entry the host's judge rates relevant should be a marked line:\n%s", reading)
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
	if !strings.Contains(reading, "Ranking source: judge "+gatherJudge+", ") || !strings.HasPrefix(readingLine(reading, laneRelevant), "! .90") {
		t.Fatalf("the prefilter judge should rank the reading:\n%s", reading)
	}
}

// Under any other host without a prefilter judge the reading ranks by the
// capture's searches run against the final draft, and the entries no search
// found by similarity to the draft, read from the search index as a search
// filled it; its header says so.
func TestCaptureDeep_AnotherHostRanksBySearchesWithoutAJudge(t *testing.T) {
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
	if !strings.Contains(reading, "by the capture's searches against the final draft") || !strings.Contains(reading, " by similarity to the draft") {
		t.Fatalf("the reading's header should name the searches and similarity as its ranking sources:\n%s", reading)
	}
	kappa := strings.Index(reading, "  "+kappaEntry+" {")
	unsearched := strings.Index(reading, "  20260603-120039-d-tac-l39 {")
	if kappa < 0 || unsearched < 0 || kappa > unsearched {
		t.Fatalf("the entry the search with the final draft found first should be listed before one no search found:\n%s", reading)
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
// time limit: the reading ranks by similarity to the draft and says so.
func TestCaptureDeep_ARankingPastItsTimeLimitLeavesSimilarityOrder(t *testing.T) {
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
	if !strings.Contains(reading, "similarity to the draft") {
		t.Fatalf("with no score the header should name similarity:\n%s", reading)
	}
	stuck.mu.Lock()
	defer stuck.mu.Unlock()
	if stuck.calls == 0 {
		t.Fatal("the stuck judge should have been asked")
	}
}

// A judge whose ranking recall over its referenced labels is below the bar
// widens the reading: the lines in doubt double, and the header says
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

	if !strings.Contains(weak, "ranking recall is below the bar, so the reading lists 200 lines in doubt") {
		t.Fatalf("the header should say the weak judge widened the reading:\n%s", weak)
	}
	if strings.Contains(sound, "below the bar") {
		t.Fatalf("a sound judge should not widen the reading:\n%s", sound)
	}
	blocks := func(reading string) int { return strings.Count(reading, "---\n\nID: ") }
	if blocks(weak) != blocks(sound) {
		t.Fatalf("the weak judge's reading serves %d entries in full against %d: widening lists more lines, never more entries in full", blocks(weak), blocks(sound))
	}
}

// storedEvents reads the session's log as the store holds it.
func storedEvents(t *testing.T, world *proctest.World, session sdd.SessionID) []sdd.StoredEvent {
	t.Helper()
	stored, err := world.Sessions.Load(t.Context(), session)
	if err != nil {
		t.Fatal(err)
	}
	return stored.Events
}

// rankingRecords reads what the staged store holds of instance's background
// ranking passes.
func rankingRecords(t *testing.T, world *proctest.World, session sdd.SessionID, instance string) []json.RawMessage {
	t.Helper()
	records, err := world.Staged.Rankings(t.Context(), sdd.SessionRef{Subject: world.Identity.Subject, Session: session}, instance)
	if err != nil {
		t.Fatal(err)
	}
	return records
}

// awaitRankingRecord waits until the staged store holds what a background
// ranking pass of instance scored.
func awaitRankingRecord(t *testing.T, world *proctest.World, session sdd.SessionID, instance string) {
	t.Helper()
	test.Eventually(t, "keeping a finished background ranking pass's scores in the staged store", func() bool {
		return len(rankingRecords(t, world, session, instance)) > 0
	})
}

// loggedRankingRecord reports whether the session's log holds a ranking
// record.
func loggedRankingRecord(t *testing.T, world *proctest.World, session sdd.SessionID) bool {
	t.Helper()
	return slices.ContainsFunc(storedEvents(t, world, session), func(e sdd.StoredEvent) bool { return e.Code == "workflow_ranking_scores" })
}

// verifyScoreReads counts the reads that credit the instance with the
// verifier's ranking scores.
func verifyScoreReads(t *testing.T, world *proctest.World, session sdd.SessionID) int {
	t.Helper()
	reads := 0
	for _, stored := range storedEvents(t, world, session) {
		if stored.Code != "workflow_event" {
			continue
		}
		var event struct {
			Event string `json:"event"`
			Data  struct {
				Tool      string `json:"tool"`
				Grounding struct {
					Prefilter *model.PrefilterPayload `json:"prefilter"`
				} `json:"grounding"`
			} `json:"data"`
		}
		if err := json.Unmarshal(stored.Payload, &event); err != nil {
			t.Fatal(err)
		}
		if event.Event == "read" && event.Data.Tool == "verify" && event.Data.Grounding.Prefilter != nil {
			reads++
		}
	}
	return reads
}

// What a background ranking pass scored outlives the server that ran it: a
// server started anew ranks the deep reading with those scores, credits them
// once and pays no pair again.
func TestCaptureDeep_ABackgroundRankingOutlivesARestart(t *testing.T) {
	host := &test.ScriptedScorer{Scores: map[string]float64{laneRelevant: 0.9}}
	world, session := newDeepWorld(t, "claude-code", sdd.VerifierPolicy{}, proctest.WithEntries(laneEntries()...),
		proctest.WithRanking(sdd.Ranking{ClaudeHost: sdd.Prefilter{Scorer: host, Judge: hostJudge}}))
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Gather(t, instance)
	proctest.RequireStep(t, session.Report(t, instance, captureDraft()), "playback")
	awaitRankingRecord(t, world, session.ID, instance)
	if loggedRankingRecord(t, world, session.ID) {
		t.Fatal("the pass's scores should stay off the session's log, which only the calls driving the session write")
	}
	paid := len(host.Scored())

	world.Restart(t)
	session, _ = world.Resume(t, session.ID, "claude-code")
	requireRecordedRanking(t, world, session, instance, host, paid)
}

// requireRecordedRanking confirms the playback and requires the deep reading
// to rank with the scores recorded before, crediting them once and asking
// the judge about nothing past the paid asks.
func requireRecordedRanking(t *testing.T, world *proctest.World, session *proctest.Session, instance string, judge *test.ScriptedScorer, paid int) {
	t.Helper()
	serve := session.Answer(t, instance, "playback", "confirm", nil, "write it")
	proctest.RequireStep(t, serve, "deepDispatch")
	reading := verifierReading(t, dispatchDeep(t, world, session, instance))
	if !strings.Contains(reading, "Ranking source: judge "+hostJudge+", ") {
		t.Fatalf("the reading's header should name the judge whose recorded scores rank it:\n%s", reading)
	}
	if !strings.HasPrefix(readingLine(reading, laneRelevant), "! .90") {
		t.Fatalf("the entry the recorded scores rate relevant should be a marked line:\n%s", reading)
	}
	if scored := judge.Scored(); len(scored) != paid {
		t.Fatalf("the judge scored %v after the restart: the recorded scores should rank the reading", scored[paid:])
	}
	if reads := verifyScoreReads(t, world, session.ID); reads != 1 {
		t.Fatalf("the reading credited the ranking scores %d times: once is enough", reads)
	}
}

// A ranking record held by the session's log ranks the deep reading as one
// held by the staged store does: a log with such records stays readable.
func TestCaptureDeep_ARankingRecordOfTheSessionLogRanksTheReading(t *testing.T) {
	host := &test.ScriptedScorer{Scores: map[string]float64{laneRelevant: 0.9}}
	world, session := newDeepWorld(t, "claude-code", sdd.VerifierPolicy{}, proctest.WithEntries(laneEntries()...),
		proctest.WithRanking(sdd.Ranking{ClaudeHost: sdd.Prefilter{Scorer: host, Judge: hostJudge}}))
	instance := session.Start(t, "capture", nil).Instance
	session.Gather(t, instance)
	proctest.RequireStep(t, session.Report(t, instance, captureDraft()), "playback")
	awaitRankingRecord(t, world, session.ID, instance)
	paid := len(host.Scored())

	var logged []sdd.StoredEvent
	for _, record := range rankingRecords(t, world, session.ID, instance) {
		payload, err := json.Marshal(map[string]any{"instance": instance, "prefilter": record})
		if err != nil {
			t.Fatal(err)
		}
		logged = append(logged, sdd.StoredEvent{CodecVersion: sdd.SessionCodecVersion, Code: "workflow_ranking_scores", Payload: payload})
	}
	stored, err := world.Sessions.Load(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = world.Sessions.Append(t.Context(), session.ID, stored.Version, sdd.SessionAppend{Events: logged})
	if err != nil {
		t.Fatal(err)
	}
	err = world.Staged.DeleteStaged(t.Context(), sdd.SessionRef{Subject: world.Identity.Subject, Session: session.ID})
	if err != nil {
		t.Fatal(err)
	}

	world.Restart(t)
	session, _ = world.Resume(t, session.ID, "claude-code")
	requireRecordedRanking(t, world, session, instance, host, paid)
}

// With a staged store that lacks the ranking part the scores of a background
// pass live in the server's memory: a server started anew asks the judge
// again, and nothing reaches the staged store.
func TestCaptureDeep_WithoutARankingStoreARestartScoresAgain(t *testing.T) {
	host := &test.ScriptedScorer{Scores: map[string]float64{laneRelevant: 0.9}}
	world, session := newDeepWorld(t, "claude-code", sdd.VerifierPolicy{}, proctest.WithEntries(laneEntries()...),
		proctest.WithRanking(sdd.Ranking{ClaudeHost: sdd.Prefilter{Scorer: host, Judge: hostJudge}}),
		proctest.WithStagedStore(func(store sdd.StagedBlobStore) sdd.StagedBlobStore { return test.BlobsOnly{StagedBlobStore: store} }))
	instance := session.Start(t, "capture", nil).Instance
	session.Gather(t, instance)
	proctest.RequireStep(t, session.Report(t, instance, captureDraft()), "playback")
	awaitScored(t, host)
	paid := len(host.Scored())

	world.Restart(t)
	session, _ = world.Resume(t, session.ID, "claude-code")
	serve := session.Answer(t, instance, "playback", "confirm", nil, "write it")
	proctest.RequireStep(t, serve, "deepDispatch")
	reading := verifierReading(t, dispatchDeep(t, world, session, instance))
	if !strings.HasPrefix(readingLine(reading, laneRelevant), "! .90") {
		t.Fatalf("the entry the judge rates relevant should be a marked line:\n%s", reading)
	}
	if scored := host.Scored(); len(scored) == paid {
		t.Fatal("a server started anew should ask the judge again: nothing kept the first pass's scores")
	}
	if records := rankingRecords(t, world, session.ID, instance); len(records) != 0 {
		t.Fatalf("the staged store should hold no ranking record, got %d", len(records))
	}
	if loggedRankingRecord(t, world, session.ID) {
		t.Fatal("the pass's scores should stay off the session's log")
	}
}

// Shutdown ends a background ranking pass still waiting on its judge: the
// claude CLI call it runs is killed with its process group, not left behind
// once the server exits.
func TestCaptureDeep_ShutdownEndsABackgroundRankingsCLI(t *testing.T) {
	cli := test.NewHangingCLI(t, "claude")
	judge, err := llmops.NewChatJudge(claude.NewRunner("sonnet", "", model.ClaudeCLICredentialsLogin))
	if err != nil {
		t.Fatal(err)
	}
	world, session := newDeepWorld(t, "claude-code", sdd.VerifierPolicy{}, proctest.WithEntries(laneEntries()...),
		proctest.WithRanking(sdd.Ranking{ClaudeHost: sdd.Prefilter{Scorer: judge, Judge: hostJudge}}))
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Gather(t, instance)
	proctest.RequireStep(t, session.Report(t, instance, captureDraft()), "playback")
	group := cli.Leader(t)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := world.App.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	test.Eventually(t, "the ranking pass's claude CLI ending with the application's shutdown", func() bool { return test.GroupGone(group) })
}
