package proctest_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/llm/relevance"
	"github.com/networkteam/sdd/test"
)

const (
	// raceSecond is the entry the ranking judge fails on first and scores on
	// its second ask, raceLate the one it fails on first and then leaves
	// unanswered.
	raceSecond = "20260603-120003-d-tac-l03"
	raceLate   = "20260603-120007-d-tac-l07"
)

// A revision reported at assemble reaches its live verifier while the ranking
// passes it starts end beside it: the pass begun as assemble passes fails on
// two entries, the reading credits what it scored, and the pass over the two
// left ends as the capture appends its next event. Neither pass's end costs
// the capture that event.
func TestCaptureDeep_ARevisionAdvancesWhileItsRankingPassesEnd(t *testing.T) {
	const connID = "deep-ranking-race"
	launcher := headlessLauncher(t)
	var (
		sessions *test.RacedSessions
		revised  atomic.Bool
		mu       sync.Mutex
		asks     = map[string]int{}
	)
	judge := &test.ScriptedScorer{Batch: 1, Default: 0.1, Script: func(_ context.Context, request relevance.Request) error {
		id := request.Candidates[0].ID
		if !revised.Load() || id != raceSecond && id != raceLate {
			return nil
		}
		mu.Lock()
		asks[id]++
		ask := asks[id]
		mu.Unlock()
		switch {
		case ask == 1:
			return errors.New("the judge's call passed its deadline")
		case id == raceSecond:
			sessions.Land(2)
			return nil
		default:
			<-sessions.Waiting()
			return errors.New("the judge's call passed its deadline")
		}
	}}
	world, session := newDeepWorld(t, connID, sdd.VerifierPolicy{Runner: "claude"}, proctest.WithDelegateLauncher(launcher), proctest.WithAssistedDialogue(), clientActor(connID),
		proctest.WithEntries(laneEntries()...),
		proctest.WithPrefilter(sdd.Prefilter{Scorer: judge, Judge: gatherJudge}),
		proctest.WithRanking(sdd.Ranking{Timeout: 300 * time.Millisecond}),
		proctest.WithSessionStore(func(store sdd.SessionStore) sdd.SessionStore {
			sessions = test.NewRacedSessions(t, store, "workflow_ranking_scores")
			return sessions
		}))
	instance := session.Start(t, "capture", nil).Instance
	session.Gather(t, instance)
	proctest.RequireStep(t, session.Report(t, instance, captureDraft()), "deepDispatch")
	session.Answer(t, instance, "deepDispatch", "delegate", nil, "")
	driveRound(t, world, launcher.NextRound(t))
	session.Refresh(t)
	proctest.RequireStep(t, session.Answer(t, instance, "deepLaunch", "started", nil, ""), "playback")
	proctest.RequireStep(t, session.Answer(t, instance, "playback", "adjust", nil, "say it happens twice"), "assemble")

	credited := verifyScoreReads(t, world, session.ID)
	sessions.Hold(session.ID)
	revised.Store(true)
	serve, err := session.ReportErr(t, instance, map[string]any{"body": "A tactical gap: the fixture observes something twice."})
	if err != nil {
		t.Fatalf("the revision's report should advance the capture, got: %v", err)
	}
	proctest.RequireStep(t, serve, "deepWait")
	if reads := verifyScoreReads(t, world, session.ID); reads == credited {
		t.Fatal("the revision's reading should credit the scores of the pass begun as assemble passed")
	}
}
