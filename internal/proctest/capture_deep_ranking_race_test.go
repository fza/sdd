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
	// unanswered until the pass's time limit.
	raceSecond = "20260603-120003-d-tac-l03"
	raceLate   = "20260603-120007-d-tac-l07"
)

// revisedCapture is a capture whose revision was reported at assemble to a
// waiting verifier.
type revisedCapture struct {
	world    *proctest.World
	session  *proctest.Session
	instance string
	// serve is what the revision's report returned.
	serve *sdd.WorkflowServe
	// credited counts the reads that credited ranking scores before the
	// revision, asked the entries the judge was asked about since.
	credited int
	asked    func() []string
}

// revisedBesideRanking drives an assisted capture through a headless
// verifier's clean round and the person's adjust back to assemble, then
// reports a revision there under a ranking judge that fails on raceSecond
// and raceLate, so the pass begun as assemble passes leaves both unscored.
func revisedBesideRanking(t *testing.T, connID string) revisedCapture {
	t.Helper()
	launcher := headlessLauncher(t)
	var (
		revised atomic.Bool
		mu      sync.Mutex
		asks    = map[string]int{}
	)
	judge := &test.ScriptedScorer{Batch: 1, Default: 0.1, Script: func(ctx context.Context, request relevance.Request) error {
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
			return nil
		default:
			<-ctx.Done()
			return ctx.Err()
		}
	}}
	world, session := newDeepWorld(t, connID, sdd.VerifierPolicy{Runner: "claude"},
		proctest.WithDelegateLauncher(launcher), proctest.WithAssistedDialogue(), clientActor(connID),
		proctest.WithEntries(laneEntries()...),
		proctest.WithPrefilter(sdd.Prefilter{Scorer: judge, Judge: gatherJudge}),
		proctest.WithRanking(sdd.Ranking{Timeout: 300 * time.Millisecond}))
	instance := session.Start(t, "capture", nil).Instance
	session.Gather(t, instance)
	proctest.RequireStep(t, session.Report(t, instance, captureDraft()), "deepDispatch")
	session.Answer(t, instance, "deepDispatch", "delegate", nil, "")
	driveRound(t, world, launcher.NextRound(t))
	session.Refresh(t)
	proctest.RequireStep(t, session.Answer(t, instance, "deepLaunch", "started", nil, ""), "playback")
	proctest.RequireStep(t, session.Answer(t, instance, "playback", "adjust", nil, "say it happens twice"), "assemble")

	capture := revisedCapture{world: world, session: session, instance: instance, credited: verifyScoreReads(t, world, session.ID)}
	before := len(judge.Scored())
	capture.asked = func() []string { return judge.Scored()[before:] }
	revised.Store(true)
	serve, err := session.ReportErr(t, instance, map[string]any{"body": "A tactical gap: the fixture observes something twice."})
	if err != nil {
		t.Fatalf("the revision's report should advance the capture, got: %v", err)
	}
	capture.serve = serve
	return capture
}

// A revision reported at assemble reaches its live verifier while the ranking
// passes it starts end beside it. What they score is kept in the staged
// store and reaches the session's log only in the capture's own read: the
// log has one writer while the capture advances.
func TestCaptureDeep_ARevisionAdvancesWhileItsRankingPassesEnd(t *testing.T) {
	capture := revisedBesideRanking(t, "deep-ranking-race")
	world, session := capture.world, capture.session.ID
	proctest.RequireStep(t, capture.serve, "deepWait")
	if reads := verifyScoreReads(t, world, session); reads == capture.credited {
		t.Fatal("the revision's reading should credit the scores of the pass begun as assemble passed")
	}
	awaitRankingRecord(t, world, session, capture.instance)
	if loggedRankingRecord(t, world, session) {
		t.Fatal("a ranking pass's scores should stay off the session's log")
	}
}
