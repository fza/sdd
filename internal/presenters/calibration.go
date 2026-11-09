package presenters

import (
	"fmt"
	"io"

	"github.com/networkteam/sdd/internal/model"
)

// RenderCalibration writes `sdd grounding status`: the judge's labels,
// recall bound, thresholds, audit and drift state, and how the pairs it
// would serve in full were answered, per reason code (D11, D33, D36).
func RenderCalibration(w io.Writer, report model.CalibrationReport) {
	fmt.Fprintf(w, "judge: %s\n", report.Judge)
	fmt.Fprintf(w, "labels: %d referenced, %d set aside (topic-only, other-scope, term-clash), %d other (counted only), %d excluded (covered, meta, unknown codes)\n",
		report.Positive, report.Negative, report.Counted, report.Excluded)
	if report.Unscored > 0 {
		fmt.Fprintf(w, "unscored: %d labelled pairs the judge has not scored; run `sdd grounding rescore` after a judge change\n", report.Unscored)
	}
	fmt.Fprintf(w, "recall bound: %.4f at %d misses of %d referenced (floor %.2f)\n", report.RecallBound, report.Misses, report.Positive, model.RecallFloor)
	if report.Thresholds.Valid {
		fmt.Fprintf(w, "thresholds: set aside below %.3f, serve in full from %.3f\n", report.Thresholds.SetAside, report.Thresholds.Relevant)
	} else {
		fmt.Fprintln(w, "thresholds: none yet, the recall bound does not hold: the prefilter sorts and sets nothing aside")
	}
	fmt.Fprintf(w, "audit: %.0f%% of set-asides shown blind; %d referenced of the latest %d audit labels (window %d, minimum %d, step above %.0f%%)\n",
		model.AuditShare*100, report.AuditMisses, report.AuditCounted, model.DriftWindow, model.DriftMinimum, model.DriftMissShare*100)
	if len(report.Steps) == 0 {
		fmt.Fprintln(w, "drift steps: none")
	} else {
		fmt.Fprintln(w, "drift steps:")
		for _, step := range report.Steps {
			fmt.Fprintf(w, "- %s: set-aside threshold to %.3f\n", step.At.UTC().Format("2006-01-02 15:04"), model.ScoreValue(step.SetAside))
		}
	}
	if report.PassedInFull == 0 {
		fmt.Fprintln(w, "precision: no labelled pair scores at or above the relevant threshold")
		return
	}
	fmt.Fprintf(w, "precision: %d of %d pairs passed in full were referenced (%.1f%%)\n", report.Referenced, report.PassedInFull, report.Precision()*100)
	for _, code := range report.SetAside {
		fmt.Fprintf(w, "- set aside as %s: %d\n", code.Code, code.Count)
	}
	if dominant := report.DominantCode(); dominant != "" {
		fmt.Fprintf(w, "prompt revision hint: most pairs passed in full were set aside as %s\n", dominant)
	}
}
