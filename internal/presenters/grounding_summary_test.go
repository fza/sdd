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
		"default on a Claude host; `llm_prefilter` would replace it")
	want := "" +
		"labels:  2,412 pairs scored by people's choices\n" +
		"         14 referenced · 1,280 dismissed implicitly (11 later flipped to bearing) · 0 set aside with a code\n" +
		"audit:   3% of the entries the threshold hides are shown to the verifier anyway, to catch a threshold that hides too much\n" +
		"         0 of the last 0 audited entries turned out referenced (window 200, needs 50 before it judges)\n" +
		"drift:   none · lowers the threshold when audited entries keep turning out relevant (more than 2% of the window)\n" +
		"judge:   chat:claude-haiku-5-5 (default on a Claude host; `llm_prefilter` would replace it)\n" +
		"         2,412 pairs unscored · recall bound 0.00 of the 0.98 needed (~188 referenced pairs) · no threshold yet\n" +
		"         uncalibrated: run `sdd grounding seed --from-refs`\n"
	if out.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}

// A calibrated judge names its thresholds, and a drift step its last move.
func TestRenderGroundingSummary_Calibrated(t *testing.T) {
	var out bytes.Buffer
	presenters.RenderGroundingSummary(&out, model.LabelTally{Labelled: 1234567}, model.CalibrationReport{
		Judge: "cohere:rerank-v3.5", RecallBound: 0.9912, AuditMisses: 3, AuditCounted: 120,
		Thresholds: model.RelevanceThresholds{Valid: true, SetAside: 0.12, Relevant: 0.7},
		Steps:      []model.DriftStep{{At: time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC), SetAside: 30}},
	}, "set by `llm_prefilter`")
	for _, want := range []string{
		"labels:  1,234,567 pairs",
		"         3 of the last 120 audited entries",
		"drift:   stepped once, last on 2026-11-01 to 0.118 · ",
		"judge:   cohere:rerank-v3.5 (set by `llm_prefilter`)\n         recall bound 0.99 of the 0.98 needed · sets aside below 0.120, serves in full from 0.700\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("status lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "uncalibrated") || strings.Contains(out.String(), "unscored") {
		t.Errorf("a calibrated judge with every pair scored should name no remedy:\n%s", out.String())
	}
}
