package model_test

import (
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

func TestTeamProofKey(t *testing.T) {
	if got := model.TeamProofKey("chat:Claude-Haiku_5.5"); got != "chat-claude-haiku-5.5" {
		t.Fatalf("key %q: a judge name becomes a plain file name", got)
	}
}

func teamFixture() model.TeamProof {
	thresholds := model.RelevanceThresholds{Valid: true, Stage: 0.98, SetAside: 0.2, Relevant: 0.9}
	states := []model.RuleState{
		{Proof: model.HidingProof{Rule: model.RuleThreshold, Checks: 53, Needed: 190, Bound: 0.98, GraphSize: 180}, Step: model.StepFull},
		{Proof: model.HidingProof{Rule: model.RuleFarFloor, Checks: 53, Needed: 190, Hidden: 1, Bound: 0.97, GraphSize: 200}, Step: model.StepNarrowed},
	}
	return model.NewTeamProof("chat:m", "r1", 210, thresholds, states, proofTime)
}

func TestNewTeamProof_HoldsCountsAndTheSmallestProvenGraph(t *testing.T) {
	team := teamFixture()
	if team.Judge != "chat:m" || team.Revision != "r1" || team.GraphSize != 180 || team.ExpiresAtSize != 225 || !team.At.Equal(proofTime) {
		t.Fatalf("record %+v: judge, revision, the smallest proven graph and its expiry", team)
	}
	if team.Thresholds != (model.TeamCut{Valid: true, Stage: 0.98, SetAside: 0.2, Relevant: 0.9}) {
		t.Fatalf("thresholds %+v", team.Thresholds)
	}
	if got := team.Rule(model.RuleFarFloor); got != (model.TeamRule{Rule: model.RuleFarFloor, Step: model.StepNarrowed, Replays: 53, Needed: 190, Hidden: 1, Bound: 0.97}) {
		t.Fatalf("floor %+v: each rule's step and replay counts", got)
	}
}

func TestTeamProof_AppliesUntilTheGraphGrewByAQuarter(t *testing.T) {
	team := teamFixture()
	for _, tc := range []struct {
		name      string
		judge     string
		revision  string
		graphSize int
		applies   bool
	}{
		{name: "the same judge", judge: "chat:m", revision: "r1", graphSize: 224, applies: true},
		{name: "a graph grown by a quarter", judge: "chat:m", revision: "r1", graphSize: 225},
		{name: "another prompt revision", judge: "chat:m", revision: "r2", graphSize: 180},
		{name: "another judge", judge: "chat:n", revision: "r1", graphSize: 180},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := team.Applies(tc.judge, tc.revision, tc.graphSize); got != tc.applies {
				t.Fatalf("applies = %v", got)
			}
		})
	}
}

func TestApplyTeamProof(t *testing.T) {
	team := teamFixture()
	local := model.RuleState{Proof: model.HidingProof{Rule: model.RuleThreshold}, Step: model.StepComputeOnly}
	got := model.ApplyTeamProof(local, &team, model.ProofRecords{}, "chat:m", "r1", 200)
	if got.Step != model.StepFull || !got.Team {
		t.Fatalf("state %+v: a machine without a local proof starts at the team's step", got)
	}
	var misses model.ProofRecords
	for _, capture := range []string{"a", "b", "c"} {
		misses.Misses = append(misses.Misses, model.Miss{Capture: capture, Rule: model.RuleThreshold, Judge: "chat:m", At: proofTime.Add(time.Hour)})
	}
	if got := model.ApplyTeamProof(local, &team, misses, "chat:m", "r1", 200); got.Step != model.StepNarrowed {
		t.Fatalf("step %s: misses from three captures since the record take it one step down", got.Step)
	}
	if got := model.ApplyTeamProof(local, &team, model.ProofRecords{}, "chat:m", "r1", 225); got.Team || got.Step != model.StepComputeOnly {
		t.Fatalf("state %+v: an expired record sets nothing", got)
	}
	proven := model.RuleState{Proof: model.HidingProof{Rule: model.RuleThreshold, Checks: 35, Needed: 35, Bound: 0.9}, Step: model.StepNarrowed}
	if got := model.ApplyTeamProof(proven, &team, model.ProofRecords{}, "chat:m", "r1", 200); got.Team || got.Step != model.StepNarrowed {
		t.Fatalf("state %+v: a local proof wins over the record", got)
	}
}

func TestTeamProofDue(t *testing.T) {
	committed := teamFixture()
	same := teamFixture()
	same.At = proofTime.Add(time.Hour)
	same.Rules[0].Replays = 60
	stepped := teamFixture()
	stepped.Rules[0].Step = model.StepNarrowed
	revised := teamFixture()
	revised.Revision = "r2"
	unproven := teamFixture()
	unproven.Rules[0].Step, unproven.Rules[1].Step = model.StepNarrowed, model.StepNarrowed
	regrown := teamFixture()
	regrown.ExpiresAtSize = 300
	for _, tc := range []struct {
		name      string
		committed *model.TeamProof
		current   model.TeamProof
		graphSize int
		due       bool
	}{
		{name: "a first full proof", current: committed, graphSize: 200, due: true},
		{name: "no full proof and no record", current: unproven, graphSize: 200},
		{name: "counts alone moved", committed: &committed, current: same, graphSize: 200},
		{name: "a step down", committed: &committed, current: stepped, graphSize: 200, due: true},
		{name: "a changed prompt", committed: &committed, current: revised, graphSize: 200, due: true},
		{name: "an expired record proven again", committed: &committed, current: regrown, graphSize: 230, due: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.TeamProofDue(tc.committed, tc.current, tc.graphSize); got != tc.due {
				t.Fatalf("due = %v", got)
			}
		})
	}
}
