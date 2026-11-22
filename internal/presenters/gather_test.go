package presenters_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
	"github.com/networkteam/sdd/internal/query"
)

// A line to judge names the newer entry that refines its entry, and never
// which reads found it.
func TestRenderGather_LineNamesTheRefinementNotTheReads(t *testing.T) {
	target := &model.Entry{ID: "20260101-100000-d-tac-tgt", Type: model.TypeDecision, Kind: model.KindDirective, Summary: "The target."}
	refining := &model.Entry{ID: "20260102-100000-d-tac-rfn", Type: model.TypeDecision, Kind: model.KindDirective, Summary: "Refines it.",
		Refs: []model.Ref{{ID: target.ID, Kind: model.RefKindRefines}}}
	g := model.NewGraph([]*model.Entry{target, refining})
	var out bytes.Buffer
	err := presenters.RenderGather(&out, &query.GatherResult{Graph: g, ToJudge: []model.GatherCandidate{
		{Entry: target, FoundBy: []string{"semantic", "keyword(x)"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "refined by "+refining.ID) {
		t.Errorf("the line should name the newer refinement:\n%s", out.String())
	}
	if strings.Contains(out.String(), "found by") || strings.Contains(out.String(), "keyword(x)") {
		t.Errorf("the line should not say which reads found the entry:\n%s", out.String())
	}
}

// The message names its cause, a time limit by its value alone, suggests
// `sdd grounding update` only where the judge lacks a stage, and hands the
// person the choice, or the agent the lines where nobody is there.
func TestJudgeShortfallText(t *testing.T) {
	base := model.JudgeShortfall{Judged: 12, Listed: 80, Minimum: 50, Waited: time.Minute}
	cases := []struct {
		name      string
		set       func(*model.JudgeShortfall)
		want      []string
		suggested bool
	}{
		{"failed", func(s *model.JudgeShortfall) {
			s.Cause, s.Failures = model.ShortfallFailed, []string{"connection refused"}
		}, []string{"the judge failed (connection refused)"}, false},
		{"slow", func(s *model.JudgeShortfall) { s.Cause = model.ShortfallSlow }, []string{"too slowly for the list within 1m0s"}, false},
		{"uncalibrated", func(s *model.JudgeShortfall) {
			s.Cause, s.Stage, s.Reachable, s.Unscored = model.ShortfallUncalibrated, 0.9, 0.95, 40
		}, []string{"recall stage 0.90", "its 40 unscored labelled pairs would give 0.95"}, true},
		{"calibrating", func(s *model.JudgeShortfall) {
			s.Cause, s.CalibrationScored, s.CalibrationOf = model.ShortfallCalibrating, 30, 90
		}, []string{"background calibration is scoring with the judge, 30 of 90 pairs scored"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shortfall := base
			tc.set(&shortfall)
			text := presenters.JudgeShortfallText(shortfall)
			for _, want := range append(tc.want, "Only 12 of 80 judged", "wait until 50 are judged", "judge the lines yourself") {
				if !strings.Contains(text, want) {
					t.Errorf("message lacks %q:\n%s", want, text)
				}
			}
			if strings.Contains(text, "sdd grounding update") != tc.suggested {
				t.Errorf("the update command should be suggested only for a judge without its stage:\n%s", text)
			}
			if strings.Contains(text, "judge_timeout") {
				t.Errorf("the time limit is named by its value alone:\n%s", text)
			}
		})
	}
	unattended := base
	unattended.Cause, unattended.Unattended = model.ShortfallSlow, true
	if text := presenters.JudgeShortfallText(unattended); strings.Contains(text, "Put the choice") || !strings.Contains(text, "judge the lines yourself") {
		t.Errorf("without a person the agent judges, no choice is put:\n%s", text)
	}
}
