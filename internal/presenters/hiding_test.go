package presenters_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
)

func TestRenderHiding(t *testing.T) {
	report := model.HidingReport{
		Judge: "chat:claude-haiku-5-5", Revision: "ab12cd34", GraphSize: 1840, Planted: 40, Detection: 0.25,
		Rules: []model.RuleReport{
			{Rule: model.RuleThreshold, State: model.RuleState{Step: model.StepFull, Proof: model.HidingProof{Rule: model.RuleThreshold, Checks: 53, Needed: 190, Bound: 0.9802, GraphSize: 1800}},
				Checks: 12, Hid: 300, Would: 300, Misses: map[model.MissDetector]int{model.DetectRescue: 1}},
			{Rule: model.RuleFarFloor, State: model.RuleState{Step: model.StepNarrowed, Proof: model.HidingProof{Rule: model.RuleFarFloor}},
				Checks: 12, Hid: 200, Would: 260, Misses: map[model.MissDetector]int{}},
		},
		Margins: model.MarginReport{Count: 446, Below: 1, Close: 5},
	}
	var out bytes.Buffer
	presenters.RenderHiding(&out, report)
	for _, want := range []string{
		"hiding:  rules hide only on a replay proof for judge chat:claude-haiku-5-5, prompt revision ab12cd34, on 1,840 live entries\n",
		"threshold: full · proof: 0 of 190 needed entries hidden in 53 replays, bound 0.980, graph of 1,800\n",
		"hid 300 in 12 checks, would have hidden 300 at full · misses: 1 rescue, 0 verifier, 0 person, 0 scan (the scan caught 25% of 40 planted)\n",
		"far-floor: narrowed · no proof yet; careful form from the first check\n",
		"hid 200 in 12 checks, would have hidden 260 at full",
		"margins: of 446 references, 1 scored below the cut and 5 within 0.05 above it; a warning only, it moves no rule\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the status lacks %q:\n%s", want, out.String())
		}
	}
}

func TestRenderHiding_NamesAnExpiredProofAndMissesSinceTheReplay(t *testing.T) {
	report := model.HidingReport{Judge: "j", Rules: []model.RuleReport{
		{Rule: model.RuleThreshold, State: model.RuleState{Step: model.StepComputeOnly, Misses: 3, Captures: 3, Proof: model.HidingProof{Rule: model.RuleThreshold, Expired: true}}, Misses: map[model.MissDetector]int{}},
	}}
	var out bytes.Buffer
	presenters.RenderHiding(&out, report)
	for _, want := range []string{"prompt revision none", "threshold: compute-only · proof expired", "3 misses from 3 captures since its latest replay", "no planted miss yet", "margins: no reference scored against a cut yet"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the status lacks %q:\n%s", want, out.String())
		}
	}
}
