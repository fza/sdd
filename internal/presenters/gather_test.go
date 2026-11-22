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

// Only a line marked likely bearing owes a verdict or a reference
// (20261121-223938-d-tac-rqi), so a line in doubt comes under a heading that
// asks for none.
func TestRenderGather_LinesInDoubtOweNoVerdict(t *testing.T) {
	marked, doubt := entry("20260101-100000-s-tac-aaa"), entry("20260101-110000-s-tac-bbb")
	result := &query.GatherResult{
		Graph: model.NewGraph([]*model.Entry{marked, doubt}),
		Lines: model.Lines{
			Bearing: []model.Line{{Entry: marked, Marked: true}},
			Doubt:   []model.Line{{Entry: doubt}},
		},
	}
	var out bytes.Buffer
	err := presenters.RenderGather(&out, result)
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "## likely bearing (1) — each owes a verdict or a reference\n") {
		t.Errorf("the marked lines should open under a heading that names what they owe:\n%s", text)
	}
	_, inDoubt, ok := strings.Cut(text, "## in doubt (1)\n")
	if !ok || strings.Contains(inDoubt, "owe") {
		t.Errorf("the lines in doubt should come under a heading that asks for no verdict:\n%s", text)
	}
	if strings.Contains(text, "to judge") {
		t.Errorf("the serve should not ask to judge each line:\n%s", text)
	}
}

// A line names the newer entry that refines its entry, and never which reads
// found it.
func TestRenderGather_LineNamesTheRefinementNotTheReads(t *testing.T) {
	target := &model.Entry{ID: "20260101-100000-d-tac-tgt", Type: model.TypeDecision, Kind: model.KindDirective, Summary: "The target.",
		Time: time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)}
	refining := &model.Entry{ID: "20260102-100000-d-tac-rfn", Type: model.TypeDecision, Kind: model.KindDirective, Summary: "Refines it.",
		Time: time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), Refs: []model.Ref{{ID: target.ID, Kind: model.RefKindRefines}}}
	g := model.NewGraph([]*model.Entry{target, refining})
	var out bytes.Buffer
	err := presenters.RenderGather(&out, &query.GatherResult{Graph: g, Reads: []model.GroundingRead{{Mode: model.GroundingKeyword, Text: "x"}},
		Lines: model.Lines{Doubt: []model.Line{{Entry: target}}}})
	if err != nil {
		t.Fatal(err)
	}
	_, inDoubt, _ := strings.Cut(out.String(), "## in doubt (1)\n")
	if !strings.Contains(inDoubt, "refined by "+refining.ID) {
		t.Errorf("the line should name the newer refinement:\n%s", out.String())
	}
	if strings.Contains(inDoubt, "found by") || strings.Contains(inDoubt, "keyword") {
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
