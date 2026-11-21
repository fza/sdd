package presenters_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
)

// An uncalibrated judge: every line of the default status, values filled.
func TestRenderGroundingSummary_Uncalibrated(t *testing.T) {
	var out bytes.Buffer
	presenters.RenderGroundingSummary(&out,
		model.LabelTally{Labelled: 2412, Referenced: 14, Implicit: 1280, Flipped: 11},
		model.CalibrationReport{Judge: "chat:claude-haiku-5-5", Unscored: 2412},
		"default on a Claude host; `llm_prefilter` would replace it", presenters.GroundingAuto{})
	want := "" +
		"labels:  2,412 pairs scored by people's choices\n" +
		"         14 referenced · 1,280 dismissed implicitly (11 later flipped to bearing) · 0 set aside with a code\n" +
		"audit:   3% of the entries the threshold hides are shown to the verifier anyway, to catch a threshold that hides too much\n" +
		"         0 of the last 0 audited entries turned out referenced (window 200, needs 50 before it judges)\n" +
		"drift:   none · lowers the threshold when audited entries keep turning out relevant (more than 2% of the window)\n" +
		"floor:   on · sets aside what the judge rates no higher than entries far from the draft in the same call\n" +
		"         0 of the last 0 audited entries it set aside turned out referenced (window 200, off above 2% of at least 50)\n" +
		"judge:   chat:claude-haiku-5-5 (default on a Claude host; `llm_prefilter` would replace it)\n" +
		"         2,412 pairs unscored · no recall stage yet (bound 0.00) · stage 0.90 needs ~35 more referenced pairs · no threshold yet\n" +
		"auto:    off (verify.auto_calibrate)\n" +
		"         uncalibrated: run `sdd grounding update`\n"
	if out.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}

// A calibrated judge names its stage, the next one, its thresholds, and a
// drift step its last move.
func TestRenderGroundingSummary_Calibrated(t *testing.T) {
	var out bytes.Buffer
	presenters.RenderGroundingSummary(&out, model.LabelTally{Labelled: 1234567}, model.CalibrationReport{
		Judge: "cohere:rerank-v3.5", RecallBound: 0.9512, PositiveWeight: 80, AuditMisses: 3, AuditCounted: 120,
		Thresholds: model.RelevanceThresholds{Valid: true, Stage: 0.95, SetAside: 0.12, Relevant: 0.7},
		Steps:      []model.DriftStep{{At: time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC), SetAside: 30}},
		Floor:      model.FloorHealth{Misses: 2, Counted: 60, Off: true},
	}, "set by `llm_prefilter`", presenters.GroundingAuto{On: true})
	for _, want := range []string{
		"labels:  1,234,567 pairs",
		"         3 of the last 120 audited entries turned out",
		"drift:   stepped once, last on 2026-11-01 to 0.118 · ",
		"floor:   off, its audit showed too many misses · ",
		"         2 of the last 60 audited entries it set aside turned out referenced",
		"judge:   cohere:rerank-v3.5 (set by `llm_prefilter`)\n         stage 0.95 of 0.90, 0.95, 0.98 (bound 0.95) · stage 0.98 needs ~109 more referenced pairs · sets aside below 0.120, serves in full from 0.700\n",
		"auto:    on (verify.auto_calibrate): `sdd serve` scores unscored labelled pairs in the background until the top stage holds\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("status lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "uncalibrated") || strings.Contains(out.String(), "unscored pairs:") {
		t.Errorf("a calibrated judge with every pair scored should name no remedy:\n%s", out.String())
	}
}

// At the top stage no next stage is named.
func TestRenderGroundingSummary_TopStage(t *testing.T) {
	var out bytes.Buffer
	presenters.RenderGroundingSummary(&out, model.LabelTally{}, model.CalibrationReport{
		Judge: "j", RecallBound: 0.99, PositiveWeight: 400,
		Thresholds: model.RelevanceThresholds{Valid: true, Stage: model.RecallFloor, SetAside: 0.1, Relevant: 0.7},
	}, "set by `llm_prefilter`", presenters.GroundingAuto{On: true})
	if !strings.Contains(out.String(), "stage 0.98 of 0.90, 0.95, 0.98 (bound 0.99) · top stage · sets aside below") {
		t.Fatalf("the top stage should say so:\n%s", out.String())
	}
}

// A run `sdd serve` records shows as running, with its stage and the pairs
// it scored, and replaces the remedy.
func TestRenderGroundingSummary_RunningInTheBackground(t *testing.T) {
	var out bytes.Buffer
	presenters.RenderGroundingSummary(&out, model.LabelTally{}, model.CalibrationReport{Judge: "j", Unscored: 40, PositiveWeight: 40,
		Thresholds: model.RelevanceThresholds{Valid: true, Stage: 0.90}},
		"set by `llm_prefilter`", presenters.GroundingAuto{On: true, Run: &model.CalibrationRun{
			Judge: "j", Started: time.Date(2026, 11, 18, 9, 30, 0, 0, time.UTC), Stage: 0.90, Scored: 12, Waiting: true,
		}})
	want := "auto:    running in `sdd serve` since 2026-11-18 09:30 UTC: 12 pairs scored, stage 0.90 held, paused until the server is idle\n"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("status lacks %q:\n%s", want, out.String())
	}
	if strings.Contains(out.String(), "run `sdd grounding update`") {
		t.Fatalf("a running background calibration should not send the reader to update:\n%s", out.String())
	}
}
