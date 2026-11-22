package presenters_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
)

// The precision line says it counts the live labels.
func TestRenderCalibration_NamesTheLabelsPrecisionCounts(t *testing.T) {
	cases := []struct {
		name   string
		report model.CalibrationReport
		want   string
	}{
		{"pairs passed in full", model.CalibrationReport{PassedInFull: 1367, Referenced: 448},
			"precision: 448 of 1367 live labelled pairs passed in full were referenced (32.8%); replayed labels not counted\n"},
		{"none passed in full", model.CalibrationReport{},
			"precision: no live labelled pair scores at or above the relevant threshold; replayed labels not counted\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var verbose bytes.Buffer
			presenters.RenderCalibration(&verbose, tc.report, presenters.GroundingAuto{})
			if !strings.Contains(verbose.String(), tc.want) {
				t.Errorf("the verbose status should name the labels the line counts:\n%s", verbose.String())
			}
		})
	}
}

func TestRenderCalibration_NamesTheFarFloor(t *testing.T) {
	for _, tc := range []struct {
		name  string
		floor model.FloorHealth
		want  string
	}{
		{"on", model.FloorHealth{Counted: 12}, "far floor: on; 0 referenced of the latest 12 audit labels of its set-asides (window 200, minimum 50, off above 2%)\n"},
		{"off", model.FloorHealth{Misses: 2, Counted: 60, Off: true}, "far floor: off, its audit showed too many misses; 2 referenced of the latest 60 audit labels of its set-asides (window 200, minimum 50, off above 2%)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var verbose bytes.Buffer
			presenters.RenderCalibration(&verbose, model.CalibrationReport{Judge: "j", Floor: tc.floor}, presenters.GroundingAuto{})
			if !strings.Contains(verbose.String(), tc.want) {
				t.Errorf("the verbose status lacks %q:\n%s", tc.want, verbose.String())
			}
		})
	}
}

// Both statuses name the labels each threshold rests on once replayed
// labels take part in the calibration, and say so when no score is served
// in full.
func TestGroundingStatus_NamesTheLabelsEachThresholdRestsOn(t *testing.T) {
	cases := []struct {
		name       string
		report     model.CalibrationReport
		thresholds model.RelevanceThresholds
		verbose    string
		summary    string
	}{
		{"live labels alone name no labels",
			model.CalibrationReport{Live: 80},
			model.RelevanceThresholds{SetAside: 0.12, Relevant: 0.7},
			"thresholds: set aside below 0.120, serve in full from 0.700\n",
			" · sets aside below 0.120, serves in full from 0.700\n"},
		{"replayed labels left out name no labels",
			model.CalibrationReport{Live: 80, Replay: 60, ReplayExcluded: true},
			model.RelevanceThresholds{SetAside: 0.12, Relevant: 0.7},
			"thresholds: set aside below 0.120, serve in full from 0.700\n",
			" · sets aside below 0.120, serves in full from 0.700\n"},
		{"the cut over all labels beside the live serve-in-full threshold",
			model.CalibrationReport{Live: 80, Replay: 60},
			model.RelevanceThresholds{SetAside: 0.2, Relevant: 0.7},
			"thresholds: set aside below 0.200 (all labels), serve in full from 0.700 (live labels)\n",
			" · sets aside below 0.200 (all labels), serves in full from 0.700 (live labels)\n"},
		{"the lower cut of the live labels",
			model.CalibrationReport{Live: 80, Replay: 60},
			model.RelevanceThresholds{SetAside: 0.078, SetAsideLive: true, Relevant: 0.7},
			"thresholds: set aside below 0.078 (live labels), serve in full from 0.700 (live labels)\n",
			" · sets aside below 0.078 (live labels), serves in full from 0.700 (live labels)\n"},
		{"no score served in full",
			model.CalibrationReport{Live: 2, Replay: 60},
			model.RelevanceThresholds{SetAside: 0.2, Relevant: 2},
			"thresholds: set aside below 0.200 (all labels), serve nothing in full (live labels)\n",
			" · sets aside below 0.200 (all labels), serves nothing in full (live labels)\n"},
		{"no score served in full by live labels alone",
			model.CalibrationReport{Live: 40},
			model.RelevanceThresholds{SetAside: 0.2, Relevant: 2},
			"thresholds: set aside below 0.200, serve nothing in full\n",
			" · sets aside below 0.200, serves nothing in full\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := tc.report
			report.Judge = "j"
			report.Thresholds = tc.thresholds
			report.Thresholds.Valid, report.Thresholds.Stage = true, 0.95
			var verbose, summary bytes.Buffer
			presenters.RenderCalibration(&verbose, report, presenters.GroundingAuto{})
			if !strings.Contains(verbose.String(), tc.verbose) {
				t.Errorf("the verbose status lacks %q:\n%s", tc.verbose, verbose.String())
			}
			presenters.RenderGroundingSummary(&summary, model.LabelTally{}, report, "set by `llm_prefilter`", presenters.GroundingAuto{})
			if !strings.Contains(summary.String(), tc.summary) {
				t.Errorf("the status lacks %q:\n%s", tc.summary, summary.String())
			}
		})
	}
}
