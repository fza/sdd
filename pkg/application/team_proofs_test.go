package application_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

// memoryTeamProofs keeps the team's records in memory and counts the writes.
type memoryTeamProofs struct {
	mu      sync.Mutex
	records map[string][]byte
	writes  []string
}

func (m *memoryTeamProofs) ReadTeamProof(_ context.Context, key string) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.records[key]
	return data, ok, nil
}

func (m *memoryTeamProofs) WriteTeamProof(_ context.Context, key string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.records == nil {
		m.records = map[string][]byte{}
	}
	m.records[key] = data
	m.writes = append(m.writes, key)
	return nil
}

func (m *memoryTeamProofs) written() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.writes)
}

// teamRecord holds a team proof for gatherJudge: the threshold at its full
// step with calibratedLedger's cut, proven on a graph of graphSize.
func teamRecord(t *testing.T, graphSize int) *memoryTeamProofs {
	t.Helper()
	thresholds := model.RelevanceThresholds{Valid: true, Stage: model.RecallFloor, SetAside: model.ScoreValue(200), Relevant: model.ScoreValue(200)}
	states := []model.RuleState{
		{Proof: model.HidingProof{Rule: model.RuleThreshold}, Step: model.StepFull},
		{Proof: model.HidingProof{Rule: model.RuleFarFloor}, Step: model.StepNarrowed},
	}
	data, err := json.Marshal(model.NewTeamProof(gatherJudge, "", graphSize, thresholds, states, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	return &memoryTeamProofs{records: map[string][]byte{model.TeamProofKey(gatherJudge): data}}
}

// A machine with an empty state folder, no label and no replay of its own
// hides at the team's committed step and thresholds, until the graph has
// grown by a quarter past the record.
func TestGather_AFreshMachineStartsAtTheTeamsProof(t *testing.T) {
	for _, tc := range []struct {
		name      string
		graphSize int
		hides     bool
	}{
		{name: "a record of this graph", graphSize: 36, hides: true},
		{name: "a record the graph outgrew", graphSize: 28},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scorer := &scriptedScorer{scores: map[string]float64{gatherAlpha: 0.95, gatherBeta: 0.0, gatherUnrelated: 0.01}}
			f := prefilterFixture(t, scorer)
			f.relevance, f.proofs, f.team = localadapter.NewMemoryRelevanceStore(), localadapter.NewMemoryProofStore(), teamRecord(t, tc.graphSize)
			result, err := gather(t, f.app(t, prefilterPolicy(1), nil), sdd.GatherRequest{Draft: "A draft about alpha."})
			if err != nil {
				t.Fatal(err)
			}
			line := setAsideLine(result.Text, model.SetAsideFiltered)
			hidden := strings.Contains(line, gatherBeta) && strings.Contains(line, gatherUnrelated)
			if hidden != tc.hides || tc.hides && slices.Contains(result.SummaryIDs, gatherBeta) {
				t.Fatalf("hidden = %v: the team's record should decide while it applies:\n%s", hidden, result.Text)
			}
		})
	}
}

// Once the machine's own replays prove a rule, the background run commits
// its standing as the team's record, once while nothing a newcomer starts
// at changes.
func TestAutoCalibrate_CommitsAFullProofAsTheTeamsRecord(t *testing.T) {
	f := replayFixture(t)
	f.proofs = floorProofs(t, 0)
	team := &memoryTeamProofs{}
	f.team = team
	f.prefilter = sdd.Prefilter{Scorer: &scriptedScorer{}, Judge: gatherJudge, Scan: &flaggingScan{}}
	policy := prefilterPolicy(1)
	policy.AutoCalibrate = true
	startAutoCalibrate(t, f.app(t, policy, &countingEmbeddings{}), sdd.AutoCalibrationRequest{})

	deadline := time.Now().Add(90 * time.Second)
	for len(team.written()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no team record was written")
		}
		time.Sleep(20 * time.Millisecond)
	}
	data, _, _ := team.ReadTeamProof(t.Context(), model.TeamProofKey(gatherJudge))
	var record model.TeamProof
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.Judge != gatherJudge || record.Rule(model.RuleFarFloor).Step != model.StepFull || record.Rule(model.RuleFarFloor).Replays != 189 || record.GraphSize == 0 || record.ExpiresAtSize <= record.GraphSize {
		t.Fatalf("record %+v: the full floor proof with its replay counts and expiry", record)
	}
	for _, forbidden := range []string{"-s-tac-", "-d-tac-", "Referenced gap"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("record %s: no entry ID and no entry text", data)
		}
	}
	time.Sleep(300 * time.Millisecond)
	if writes := team.written(); len(writes) != 1 {
		t.Fatalf("writes %v: an unchanged standing is written once", writes)
	}
}
