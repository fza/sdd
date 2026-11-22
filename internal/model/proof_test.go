package model_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

var proofTime = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

// replays returns count checks of judge "j" at revision "r" on a graph of
// 100 entries, each with one needed candidate scored needed and one other
// scored 10, every call's far line at lowest 20 and median 40.
func replays(count int, needed uint8) []model.ReplayCheck {
	var checks []model.ReplayCheck
	for i := range count {
		checks = append(checks, model.ReplayCheck{
			Subject: fmt.Sprintf("s%03d", i), Judge: "j", Revision: "r", At: proofTime.Add(time.Duration(i) * time.Minute), GraphSize: 100,
			Candidates: []model.ReplayCandidate{
				{ID: fmt.Sprintf("n%03d", i), Score: needed, Far: true, Lowest: 20, Median: 40, Needed: true},
				{ID: fmt.Sprintf("o%03d", i), Score: 10, Far: true, Lowest: 20, Median: 40},
			},
		})
	}
	return checks
}

func TestProveRule(t *testing.T) {
	thresholds := model.RelevanceThresholds{Valid: true, SetAside: model.ScoreValue(30)}
	for _, tc := range []struct {
		name          string
		rule          model.HidingRule
		checks        []model.ReplayCheck
		graphSize     int
		revision      string
		thresholds    model.RelevanceThresholds
		needed, hiddn int
		step          model.HidingStep
		expired       bool
	}{
		{name: "the floor over 189 needed entries it spares", rule: model.RuleFarFloor, checks: replays(189, 41), graphSize: 100, revision: "r", thresholds: thresholds, needed: 189, step: model.StepFull},
		{name: "the floor one short of full", rule: model.RuleFarFloor, checks: replays(188, 41), graphSize: 100, revision: "r", thresholds: thresholds, needed: 188, step: model.StepNarrowed},
		{name: "the floor hiding at its median line", rule: model.RuleFarFloor, checks: replays(189, 40), graphSize: 100, revision: "r", thresholds: thresholds, needed: 189, hiddn: 189, step: model.StepNarrowed},
		{name: "the threshold over 35 needed entries it keeps", rule: model.RuleThreshold, checks: replays(35, 30), graphSize: 100, revision: "r", thresholds: thresholds, needed: 35, step: model.StepNarrowed},
		{name: "the threshold over 34", rule: model.RuleThreshold, checks: replays(34, 30), graphSize: 100, revision: "r", thresholds: thresholds, needed: 34, step: model.StepComputeOnly},
		{name: "the threshold hiding below its cut", rule: model.RuleThreshold, checks: replays(189, 29), graphSize: 100, revision: "r", thresholds: thresholds, needed: 189, hiddn: 189, step: model.StepComputeOnly},
		{name: "the threshold without a cut", rule: model.RuleThreshold, checks: replays(189, 200), graphSize: 100, revision: "r", step: model.StepComputeOnly},
		{name: "a graph grown by a quarter", rule: model.RuleThreshold, checks: replays(189, 200), graphSize: 125, revision: "r", thresholds: thresholds, step: model.StepComputeOnly, expired: true},
		{name: "a graph grown by less", rule: model.RuleThreshold, checks: replays(189, 200), graphSize: 124, revision: "r", thresholds: thresholds, needed: 189, step: model.StepFull},
		{name: "a changed prompt", rule: model.RuleThreshold, checks: replays(189, 200), graphSize: 100, revision: "r2", thresholds: thresholds, step: model.StepComputeOnly, expired: true},
		{name: "an expired floor falls back to careful", rule: model.RuleFarFloor, checks: replays(189, 200), graphSize: 125, revision: "r", thresholds: thresholds, step: model.StepNarrowed, expired: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := model.HidingState(tc.rule, model.ProofRecords{Replays: tc.checks}, "j", tc.revision, tc.graphSize, tc.thresholds)
			proof := state.Proof
			if proof.Needed != tc.needed || proof.Hidden != tc.hiddn || proof.Expired != tc.expired {
				t.Fatalf("proof %+v: needed, hidden or expiry wrong", proof)
			}
			if state.Step != tc.step {
				t.Fatalf("step %s, bound %.4f", state.Step, proof.Bound)
			}
			if proof.Needed > 0 && (proof.Judge != "j" || proof.Revision != tc.revision || proof.GraphSize != 100 || proof.Latest.IsZero()) {
				t.Fatalf("proof %+v should name judge, revision, graph size and its latest check", proof)
			}
		})
	}
}

func TestHidingState_StepsDownOnMissesFromDistinctCaptures(t *testing.T) {
	thresholds := model.RelevanceThresholds{Valid: true, SetAside: model.ScoreValue(30)}
	checks := replays(189, 200)
	latest := checks[len(checks)-1].At
	miss := func(capture string, at time.Time) model.Miss {
		return model.Miss{Capture: capture, Hidden: "h-" + capture, Rule: model.RuleThreshold, Detector: model.DetectRescue, Judge: "j", At: at}
	}
	for _, tc := range []struct {
		name   string
		misses []model.Miss
		step   model.HidingStep
	}{
		{name: "one miss moves no rule", misses: []model.Miss{miss("c1", latest.Add(time.Hour))}, step: model.StepFull},
		{name: "three misses from one capture", misses: []model.Miss{miss("c1", latest.Add(time.Hour)), miss("c1", latest.Add(time.Hour)), miss("c1", latest.Add(time.Hour))}, step: model.StepFull},
		{name: "three misses from three captures", misses: []model.Miss{miss("c1", latest.Add(time.Hour)), miss("c2", latest.Add(time.Hour)), miss("c3", latest.Add(time.Hour))}, step: model.StepNarrowed},
		{name: "six from six", misses: []model.Miss{miss("c1", latest.Add(1)), miss("c2", latest.Add(1)), miss("c3", latest.Add(1)), miss("c4", latest.Add(1)), miss("c5", latest.Add(1)), miss("c6", latest.Add(1))}, step: model.StepComputeOnly},
		{name: "misses before the latest replay", misses: []model.Miss{miss("c1", latest), miss("c2", latest), miss("c3", latest.Add(-time.Hour))}, step: model.StepFull},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := model.HidingState(model.RuleThreshold, model.ProofRecords{Replays: checks, Misses: tc.misses}, "j", "r", 100, thresholds)
			if state.Step != tc.step {
				t.Fatalf("step %s after %d misses from %d captures", state.Step, state.Misses, state.Captures)
			}
		})
	}
}

func TestSteppedThresholdsAndFloorForm(t *testing.T) {
	thresholds := model.RelevanceThresholds{Valid: true, SetAside: 0.4, Relevant: 0.9}
	for _, tc := range []struct {
		step     model.HidingStep
		setAside float64
		form     model.FarForm
	}{
		{step: model.StepFull, setAside: 0.4, form: model.FarMedian},
		{step: model.StepNarrowed, setAside: 0.2, form: model.FarCareful},
		{step: model.StepComputeOnly, setAside: 0, form: model.FarCareful},
	} {
		t.Run(tc.step.String(), func(t *testing.T) {
			got := model.SteppedThresholds(thresholds, tc.step)
			if got.SetAside != tc.setAside || got.Relevant != thresholds.Relevant || !got.Valid {
				t.Fatalf("thresholds %+v: the cut should follow the step and serve-in-full stay", got)
			}
			if form := model.FloorForm(tc.step); form != tc.form {
				t.Fatalf("form %d: the median form needs the full step", form)
			}
		})
	}
}

func TestDetectionRate(t *testing.T) {
	checks := []model.ReplayCheck{{Planted: "a", Caught: true}, {Planted: "b"}, {Planted: "c"}, {}}
	rate, planted := model.DetectionRate(checks)
	if planted != 3 || rate != 1.0/3 {
		t.Fatalf("rate %v of %d: one of three planted misses was caught", rate, planted)
	}
}

func TestCaptureProofs_TalliesTheCheckAndRecordsRescueAndVerifierMisses(t *testing.T) {
	reading := &model.Reading{}
	reading.Fold(model.ReadPayload{Prefilter: &model.PrefilterPayload{
		Judge: "j", Subject: "s", Cut: 100,
		Scores:   map[string]uint8{"t1": 90, "t2": 50, "f1": 10, "f2": 30, "ref": 120, "close": 104},
		SetAside: []string{"t1", "t2"}, Below: []string{"f1", "f2"},
		Hiding: []model.RuleMark{{Rule: model.RuleThreshold, Step: model.StepFull, Would: 2}, {Rule: model.RuleFarFloor, Step: model.StepNarrowed, Would: 3}},
	}})
	findings := []model.DeepFinding{{Basis: []string{"f2", "t2"}}}
	records := model.CaptureProofs("e", reading, model.CaptureEvidence{Answered: []string{"t2", "ref", "close"}, Findings: findings}, proofTime)
	if len(records.Tallies) != 1 {
		t.Fatalf("tallies %+v: one per judge", records.Tallies)
	}
	tally := records.Tallies[0]
	want := []model.RuleTally{
		{Rule: model.RuleThreshold, Step: model.StepFull, Hidden: []string{"t1", "t2"}, Would: 2},
		{Rule: model.RuleFarFloor, Step: model.StepNarrowed, Hidden: []string{"f2", "f1"}, Would: 3},
	}
	if fmt.Sprint(tally.Rules) != fmt.Sprint(want) {
		t.Fatalf("rules %+v: each rule's step, its hidden entries closest to the line first, and what it would hide", tally.Rules)
	}
	margins := fmt.Sprintf("%.3f", tally.Margins)
	if margins != "[-0.196 0.078 0.016]" {
		t.Fatalf("margins %s: each answered entry's score minus the cut", margins)
	}
	got := map[string]model.MissDetector{}
	for _, miss := range records.Misses {
		got[miss.Hidden] = miss.Detector
		if miss.Capture != "e" || miss.Judge != "j" || !miss.At.Equal(proofTime) {
			t.Fatalf("miss %+v should name capture, judge and time", miss)
		}
	}
	if len(got) != 2 || got["t2"] != model.DetectRescue || got["f2"] != model.DetectVerifier {
		t.Fatalf("misses %+v: a referenced hidden entry is a rescue, one a finding rests on a verifier miss, each once", records.Misses)
	}
}

func TestWouldSetAside(t *testing.T) {
	candidates := []model.GatherCandidate{{Entry: &model.Entry{ID: "a"}}, {Entry: &model.Entry{ID: "b"}}, {Entry: &model.Entry{ID: "c"}}, {Entry: &model.Entry{ID: "d"}}}
	scores := map[string]uint8{"a": 10, "b": 10, "c": 200}
	thresholds := model.RelevanceThresholds{Valid: true, SetAside: model.ScoreValue(100)}
	if got := model.WouldSetAside(candidates, scores, map[string]bool{"b": true}, thresholds); got != 1 {
		t.Fatalf("would %d: a low score not below the floor, scored, counts", got)
	}
	if got := model.WouldSetAside(candidates, scores, nil, model.RelevanceThresholds{}); got != 0 {
		t.Fatalf("would %d: no cut sets nothing aside", got)
	}
}

func TestNewHidingReport(t *testing.T) {
	thresholds := model.RelevanceThresholds{Valid: true, SetAside: model.ScoreValue(30)}
	records := model.ProofRecords{
		Replays: append(replays(35, 200), model.ReplayCheck{Judge: "j", Revision: "r", GraphSize: 100, Planted: "x", Caught: true, At: proofTime}),
		Tallies: []model.CheckTally{
			{Judge: "j", Rules: []model.RuleTally{{Rule: model.RuleThreshold, Hidden: []string{"a", "b"}, Would: 5}}, Margins: []float64{-0.1, 0.03, 0.5}},
			{Judge: "other", Rules: []model.RuleTally{{Rule: model.RuleThreshold, Hidden: []string{"c"}}}, Margins: []float64{0.01}},
		},
		Misses: []model.Miss{{Rule: model.RuleThreshold, Detector: model.DetectScan, Judge: "j"}},
	}
	report := model.NewHidingReport(records, "j", "r", 100, thresholds)
	threshold := report.Rules[0]
	if threshold.Rule != model.RuleThreshold || threshold.Checks != 1 || threshold.Hid != 2 || threshold.Would != 5 || threshold.Misses[model.DetectScan] != 1 {
		t.Fatalf("threshold %+v: the judge's tallies and misses alone count", threshold)
	}
	if threshold.State.Step != model.StepNarrowed || report.Replays != 36 || report.Planted != 1 || report.Detection != 1 {
		t.Fatalf("report %+v: step, replays and detection rate", report)
	}
	if report.Margins != (model.MarginReport{Count: 3, Below: 1, Close: 1}) || report.Cut != thresholds.SetAside {
		t.Fatalf("margins %+v: one below the cut and one within the warning band", report.Margins)
	}
}

func TestCaptureProofs_ThePersonsWordsNameAHiddenEntry(t *testing.T) {
	const (
		full  = "20260101-100000-s-tac-ful"
		short = "20260101-100001-d-cpt-sho"
		other = "20260101-100002-s-tac-oth"
	)
	reading := &model.Reading{}
	reading.Fold(model.ReadPayload{Prefilter: &model.PrefilterPayload{
		Judge: "j", Subject: "s", Scores: map[string]uint8{full: 10, short: 20, other: 30},
		Below: []string{full, short, other}, Hiding: []model.RuleMark{{Rule: model.RuleFarFloor, Step: model.StepNarrowed}},
	}})
	words := []string{"Write it, but did you see " + full + "?", "Yes. Also d-cpt-sho bears on it."}
	records := model.CaptureProofs("e", reading, model.CaptureEvidence{Answered: []string{other}, Words: words}, proofTime)
	got := map[string]model.MissDetector{}
	for _, miss := range records.Misses {
		got[miss.Hidden] = miss.Detector
	}
	want := map[string]model.MissDetector{full: model.DetectPerson, short: model.DetectPerson, other: model.DetectRescue}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("misses %v: an entry the person names by full or short ID is a person miss, a reference stays a rescue", got)
	}
}

func TestNamedEntries(t *testing.T) {
	named := model.NamedEntries([]string{"see 20260101-100000-s-tac-ful and d-cpt-sho, not re-run or x-y"})
	if len(named) != 2 || !named["20260101-100000-s-tac-ful"] || !named["d-cpt-sho"] || named["s-tac-ful"] {
		t.Fatalf("named %v: full and short IDs, a full one not again as short", named)
	}
}
