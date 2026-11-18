package presenters

import (
	"fmt"
	"io"

	"github.com/networkteam/sdd/internal/model"
)

// RenderCalibration writes `sdd grounding status`: the judge's labels, recall
// bound, thresholds, audit and drift state, and how the pairs it would serve
// in full were answered, per reason code.
func RenderCalibration(w io.Writer, report model.CalibrationReport) {
	fmt.Fprintf(w, "judge: %s\n", report.Judge)
	sources := fmt.Sprintf("sources: %d live labels, %d replayed from past captures", report.Live, report.Replay)
	if report.ReplayExcluded {
		sources += "; the report below reads the live labels alone (--exclude-replay)"
	}
	fmt.Fprintln(w, sources)
	fmt.Fprintf(w, "labels: %d referenced or flipped, %d set aside (topic-only, other-scope, term-clash, implicit), %d other (counted only), %d excluded (covered, meta, no or unknown codes); weights: referenced 1, opened 0.5, explicit dismissal 1, implicit dismissal 0.25\n",
		report.Positive, report.Negative, report.Counted, report.Excluded)
	fmt.Fprintf(w, "implicit dismissals: %d recorded, %d flipped to bearing (flip rate %.1f%%)\n", report.Implicit, report.Flipped, report.FlipRate()*100)
	if report.Unscored > 0 {
		fmt.Fprintf(w, "unscored: %d labelled pairs the judge has not scored; run `sdd grounding rescore` after a judge change\n", report.Unscored)
	}
	fmt.Fprintf(w, "recall bound: %.4f at %d misses of %d referenced (floor %.2f)\n", report.RecallBound, report.Misses, report.Positive, model.RecallFloor)
	if report.Thresholds.Valid {
		fmt.Fprintf(w, "thresholds: set aside below %.3f, serve in full from %.3f\n", report.Thresholds.SetAside, report.Thresholds.Relevant)
	} else {
		fmt.Fprintln(w, "thresholds: none yet, the recall bound does not hold: the prefilter sorts and sets nothing aside")
		fmt.Fprintln(w, "uncalibrated: run `sdd grounding seed --from-refs` to label the graph's own references and score them")
	}
	ranking := report.Ranking
	switch {
	case !ranking.Measured:
		fmt.Fprintf(w, "ranking recall: not measured, %d referenced labels of the %d it takes; run `sdd grounding seed --from-refs`\n", ranking.Positives, model.RankingRecallMinimum)
	case ranking.Weak():
		fmt.Fprintf(w, "ranking recall: %.2f of %d referenced labels score at or above %.1f, below the bar %.2f: the judge ranks poorly, so the deep reading serves the top %d in full and lists the next %d with their summary\n",
			ranking.Recall, ranking.Positives, model.RankingRecallLine, model.RankingRecallBar, 2*model.DeepTopRanked, 2*model.DeepSummaryRanked)
	default:
		fmt.Fprintf(w, "ranking recall: %.2f of %d referenced labels score at or above %.1f (bar %.2f)\n", ranking.Recall, ranking.Positives, model.RankingRecallLine, model.RankingRecallBar)
	}
	fmt.Fprintf(w, "audit: %.0f%% of set-asides shown blind; %d referenced of the latest %d audit labels (window %d, minimum %d, step above %.0f%%)\n",
		model.AuditShare*100, report.AuditMisses, report.AuditCounted, model.DriftWindow, model.DriftMinimum, model.DriftMissShare*100)
	fmt.Fprintf(w, "opened hidden: %d entries the deep verifier opened from the ones its reading hid, no finding resting on them (no drift effect)\n", report.OpenedHidden)
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
