package presenters

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/networkteam/sdd/internal/model"
)

// GroundingAuto is the background calibration a status names: On while
// verify.auto_calibrate is on, Run the run `sdd serve` records, nil while
// none runs.
type GroundingAuto struct {
	On  bool
	Run *model.CalibrationRun
}

// choices gave, the ranking judge's calibration and its background
// calibration. origin says where the judge comes from.
// where the judge comes from.
func RenderGroundingSummary(w io.Writer, tally model.LabelTally, report model.CalibrationReport, origin string, auto GroundingAuto) {
	const indent = "         "
	fmt.Fprintf(w, "labels:  %s pairs scored by people's choices\n", thousands(tally.Labelled))
	fmt.Fprintf(w, "%s%s referenced · %s dismissed implicitly (%s later flipped to bearing) · %s set aside with a code\n",
		indent, thousands(tally.Referenced), thousands(tally.Implicit), thousands(tally.Flipped), thousands(tally.Coded))
	fmt.Fprintf(w, "judge:   %s (%s)\n", report.Judge, origin)
	var state []string
	if report.Unscored > 0 {
		state = append(state, thousands(report.Unscored)+" pairs unscored")
	}
	if report.Thresholds.Valid {
		state = append(state, fmt.Sprintf("stage %.2f of %s (bound %.2f)", report.Thresholds.Stage, stageList(), report.RecallBound))
	} else {
		state = append(state, fmt.Sprintf("no recall stage yet (bound %.2f)", report.RecallBound))
	}
	if next, needed := report.NextStage(); next > 0 {
		state = append(state, fmt.Sprintf("stage %.2f needs ~%.0f more referenced pairs", next, math.Ceil(needed)))
	} else {
		state = append(state, "top stage")
	}
	if report.Thresholds.Valid {
		state = append(state, thresholdsText(report, "s"))
	} else {
		state = append(state, "no threshold yet")
	}
	fmt.Fprintf(w, "%s%s\n", indent, strings.Join(state, " · "))
	fmt.Fprintf(w, "auto:    %s\n", autoCalibrationLine(auto))
	if auto.Run != nil {
		return
	}
	switch {
	case !report.Thresholds.Valid:
		fmt.Fprintf(w, "%suncalibrated: run `sdd grounding update`\n", indent)
	case report.Unscored > 0:
		fmt.Fprintf(w, "%sunscored pairs: run `sdd grounding update`\n", indent)
	}
}

// thresholdsText names a calibrated report's thresholds, its verbs ending in
// s, and the labels each rests on (model.Calibrate) once replayed ones take
// part in the calibration.
func thresholdsText(report model.CalibrationReport, s string) string {
	thresholds := report.Thresholds
	setAside, relevant := "", ""
	if report.Replay > 0 && !report.ReplayExcluded {
		setAside, relevant = " (all labels)", " (live labels)"
		if thresholds.SetAsideLive {
			setAside = relevant
		}
	}
	serve := fmt.Sprintf("serve%s in full from %.3f", s, thresholds.Relevant)
	if !thresholds.ServesInFull() {
		serve = "serve" + s + " nothing in full"
	}
	return fmt.Sprintf("set%s aside below %.3f%s, %s%s", s, thresholds.SetAside, setAside, serve, relevant)
}

// stageList names the recall stages, rising.
func stageList() string {
	var stages []string
	for _, stage := range model.RecallStages() {
		stages = append(stages, fmt.Sprintf("%.2f", stage))
	}
	return strings.Join(stages, ", ")
}

// autoCalibrationLine says whether the background calibration runs, what
// it does and how far it came.
func autoCalibrationLine(auto GroundingAuto) string {
	if run := auto.Run; run != nil {
		held := "no stage held yet"
		if run.Stage > 0 {
			held = fmt.Sprintf("stage %.2f held", run.Stage)
		}
		doing := thousands(run.Scored) + " pairs scored"
		if run.Replaying {
			doing = "replaying past entries and scanning written ones"
		}
		line := fmt.Sprintf("running in `sdd serve` since %s: %s, %s", run.Started.UTC().Format("2006-01-02 15:04 UTC"), doing, held)
		if run.Waiting {
			line += ", paused until the server is idle"
		}
		return line
	}
	if !auto.On {
		return "off (verify.auto_calibrate)"
	}
	return "on (verify.auto_calibrate): `sdd serve` scores unscored labelled pairs until the top stage holds, then replays past entries and scans written ones in the background"
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
