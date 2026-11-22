package proctest_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/llm/relevance"
	"github.com/networkteam/sdd/test"
)

// A check whose judge stays short of the minimum in a session without a
// person puts no choice: the agent judges from the lines, and the session
// log records the shortfall.
func TestCaptureGrounding_AnUnattendedCheckShortOfItsMinimumHasTheAgentJudge(t *testing.T) {
	failing := &test.ScriptedScorer{Script: func(context.Context, relevance.Request) error { return errors.New("provider unavailable") }}
	world := proctest.NewWorld(t, proctest.WithEntries(captureFixtureEntries()...),
		proctest.WithGrounding(sdd.GroundingPolicy{Enforce: true, Prefilter: true, PrefilterFrom: 1}),
		proctest.WithPrefilter(sdd.Prefilter{Scorer: failing, Judge: "test:scripted"}))
	world.SetDialogue("assisted")
	session := world.Open(t, "claude-code")
	instance := session.Start(t, "capture", nil).Instance

	session.Report(t, instance, captureDraft())
	gathered := session.Gather(t, instance)
	if !strings.Contains(gathered.Text, "No person is at this check to choose: judge the lines yourself.") || strings.Contains(gathered.Text, "Put the choice") {
		t.Fatalf("an unattended check short of its minimum should leave the lines to the agent:\n%s", gathered.Text)
	}
	log, err := os.ReadFile(filepath.Join(world.SessionsDir, string(session.ID)+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), `"shortfall":{`) || !strings.Contains(string(log), `"unattended":true`) {
		t.Fatalf("the session log should record the shortfall:\n%s", log)
	}
}
