package presenters

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/networkteam/sdd/internal/model"
)

// RenderGroundingSummary writes `sdd grounding status`: the labels people's
// choices gave, the audit and drift rules over the ranking judge's labels,
// and that judge's calibration. origin says where the judge comes from.
func RenderGroundingSummary(w io.Writer, tally model.LabelTally, report model.CalibrationReport, origin string) {
	const indent = "         "
	fmt.Fprintf(w, "labels:  %s pairs scored by people's choices\n", thousands(tally.Labelled))
	fmt.Fprintf(w, "%s%s referenced · %s dismissed implicitly (%s later flipped to bearing) · %s set aside with a code\n",
		indent, thousands(tally.Referenced), thousands(tally.Implicit), thousands(tally.Flipped), thousands(tally.Coded))
	fmt.Fprintf(w, "audit:   %.0f%% of the entries the threshold hides are shown to the verifier anyway, to catch a threshold that hides too much\n", model.AuditShare*100)
	fmt.Fprintf(w, "%s%s of the last %s audited entries turned out referenced (window %d, needs %d before it judges)\n",
		indent, thousands(report.AuditMisses), thousands(report.AuditCounted), model.DriftWindow, model.DriftMinimum)
	drift := "none"
	if n := len(report.Steps); n > 0 {
		last := report.Steps[n-1]
		times := "once"
		if n > 1 {
			times = strconv.Itoa(n) + " times"
		}
		drift = fmt.Sprintf("stepped %s, last on %s to %.3f", times, last.At.UTC().Format("2006-01-02"), model.ScoreValue(last.SetAside))
	}
	fmt.Fprintf(w, "drift:   %s · lowers the threshold when audited entries keep turning out relevant (more than %.0f%% of the window)\n", drift, model.DriftMissShare*100)
	fmt.Fprintf(w, "judge:   %s (%s)\n", report.Judge, origin)
	var state []string
	if report.Unscored > 0 {
		state = append(state, thousands(report.Unscored)+" pairs unscored")
	}
	if report.Thresholds.Valid {
		state = append(state, fmt.Sprintf("recall bound %.2f of the %.2f needed", report.RecallBound, model.RecallFloor),
			fmt.Sprintf("sets aside below %.3f, serves in full from %.3f", report.Thresholds.SetAside, report.Thresholds.Relevant))
	} else {
		state = append(state, fmt.Sprintf("recall bound %.2f of the %.2f needed (~%.0f referenced pairs)", report.RecallBound, model.RecallFloor, model.RecallFloorPositives()),
			"no threshold yet")
	}
	fmt.Fprintf(w, "%s%s\n", indent, strings.Join(state, " · "))
	switch {
	case !report.Thresholds.Valid:
		fmt.Fprintf(w, "%suncalibrated: run `sdd grounding update`\n", indent)
	case report.Unscored > 0:
		fmt.Fprintf(w, "%sunscored pairs: run `sdd grounding update`\n", indent)
	}
}

// thousands writes n with a comma between each group of three digits.
func thousands(n int) string {
	digits := strconv.Itoa(n)
	if n < 0 {
		return "-" + thousands(-n)
	}
	var b strings.Builder
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(digit)
	}
	return b.String()
}
