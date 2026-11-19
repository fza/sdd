package proctest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// stockSessionID names the session in testdata/stock_capture_session.jsonl:
// a log as a stock build writes it, its capture i_2 left at playback.
const stockSessionID = sdd.SessionID("s_20261119-015858-293ca6d43e051a379b5624653899af32")

func stockCaptureWorld(t *testing.T) *proctest.World {
	t.Helper()
	world := proctest.NewWorld(t, proctest.WithEntries(captureFixtureEntries()...))
	log, err := os.ReadFile(filepath.Join("testdata", "stock_capture_session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(world.SessionsDir, string(stockSessionID)+".jsonl"), log, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	return world
}

func openServe(t *testing.T, result sdd.WorkflowResumeResult, instance string) *sdd.WorkflowServe {
	t.Helper()
	for i := range result.Open {
		if result.Open[i].Instance == instance {
			return &result.Open[i]
		}
	}
	return nil
}

// A move another sdd build started is listed with abandon as its only choice:
// no step serves, so the agent neither loops on it nor parks it.
func TestCapture_StockStartedOffersOnlyAbandon(t *testing.T) {
	world := stockCaptureWorld(t)
	session, resumed := world.Resume(t, stockSessionID, "fork-client")

	capture := openServe(t, resumed, "i_2")
	if capture == nil || capture.Procedure != "capture" {
		t.Fatalf("the stock capture should be listed, got %+v", resumed.Open)
	}
	if capture.PendingChooser != nil || capture.ReportSchema != nil {
		t.Errorf("the stock capture should offer no step to continue, got chooser %+v schema %v", capture.PendingChooser, capture.ReportSchema)
	}
	if !strings.Contains(capture.Instructions, "abandon") || strings.Contains(capture.Instructions, "Play the draft back") {
		t.Errorf("the stock capture should name abandon as its only choice, got %q", capture.Instructions)
	}

	_, err := session.ReportErr(t, "i_2", map[string]any{"body": "A tactical gap: revised."})
	if err == nil || !strings.Contains(err.Error(), "abandon") {
		t.Errorf("a report on the stock capture should be refused naming abandon, got %v", err)
	}
	_, err = session.AnswerErr(t, "i_2", "playback", "confirm", nil, "yes, record it")
	if err == nil || !strings.Contains(err.Error(), "abandon") {
		t.Errorf("an answer on the stock capture should be refused naming abandon, got %v", err)
	}
	_, err = session.WF.Park(t.Context(), world.Identity, "i_2", "later")
	if err == nil || !strings.Contains(err.Error(), "abandon") {
		t.Errorf("parking the stock capture should be refused naming abandon, got %v", err)
	}

	_, err = session.WF.Abandon(t.Context(), world.Identity, "i_2", "started by another sdd build")
	if err != nil {
		t.Fatalf("abandoning the stock capture should pass, got %v", err)
	}
	world.Restart(t)
	_, resumed = world.Resume(t, stockSessionID, "fork-client")
	if serve := openServe(t, resumed, "i_2"); serve != nil {
		t.Errorf("the abandoned capture should leave the listing, got %+v", serve)
	}
}

// A move the fork starts in a session another build opened continues as any
// fork move, across a restart too.
func TestCapture_ForkMoveInAStockSessionContinues(t *testing.T) {
	world := stockCaptureWorld(t)
	session, _ := world.Resume(t, stockSessionID, "fork-client")
	started := session.Start(t, "capture", nil)
	proctest.RequireStep(t, started, "assemble")

	world.Restart(t)
	_, resumed := world.Resume(t, stockSessionID, "fork-client")
	capture := openServe(t, resumed, started.Instance)
	if capture == nil || capture.ReportSchema == nil {
		t.Fatalf("the fork's own capture should serve its step after a restart, got %+v", capture)
	}
	if stock := openServe(t, resumed, "i_2"); stock == nil || stock.ReportSchema != nil {
		t.Errorf("the stock capture should stay abandon-only beside it, got %+v", stock)
	}
}
