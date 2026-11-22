package application_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/networkteam/slogutils"

	sdd "github.com/networkteam/sdd/pkg/application"
)

// A judge that fails every call leaves the check short of its minimum: the
// serve says how many were judged of how many and why, and hands the person
// the choice; the server log records the shortfall.
func TestGather_AJudgeShortOfTheMinimumNamesItAndTheChoice(t *testing.T) {
	f := newGatherFixture(t)
	f.seed(t)
	f.prefilter = sdd.Prefilter{Scorer: &scriptedScorer{failAll: true}, Judge: gatherJudge}
	application := f.app(t, prefilterPolicy(1), nil)
	var logs bytes.Buffer
	ctx := slogutils.WithLogger(t.Context(), slog.New(slog.NewTextHandler(&logs, nil)))

	result, err := application.Gather(ctx, sdd.RequestIdentity{Subject: "tester"}, gatherProject, sdd.GatherRequest{SyncMode: sdd.SearchSyncAll, Draft: "A draft about alpha.", Terms: []string{"alpha"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# judged too few", "Only 0 of ", "the judge failed (provider unavailable)", "Put the choice to the person", "judge the lines yourself"} {
		if !strings.Contains(result.Text, want) {
			t.Errorf("serve lacks %q:\n%s", want, result.Text)
		}
	}
	if strings.Contains(result.Text, "sdd grounding update") {
		t.Errorf("a failing judge gets no update suggestion:\n%s", result.Text)
	}
	if !strings.Contains(logs.String(), "fewer candidates than the minimum") || !strings.Contains(logs.String(), "cause=failed") {
		t.Errorf("the server log should record the shortfall:\n%s", logs.String())
	}
}

// A judge that scores every candidate of a list shorter than the minimum
// leaves no shortfall.
func TestGather_AJudgeThatScoresTheWholeShortListIsNotShort(t *testing.T) {
	f := newGatherFixture(t)
	f.seed(t)
	f.prefilter = sdd.Prefilter{Scorer: &scriptedScorer{}, Judge: gatherJudge}
	result, err := gather(t, f.app(t, prefilterPolicy(1), nil), sdd.GatherRequest{Draft: "A draft about alpha.", Terms: []string{"alpha"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Text, "judged too few") {
		t.Errorf("a whole short list judged is no shortfall:\n%s", result.Text)
	}
}
