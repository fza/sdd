package model_test

import (
	"fmt"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func cosineLabels(scores ...uint8) []model.RelevanceLabel {
	var labels []model.RelevanceLabel
	for i, score := range scores {
		labels = append(labels, model.RelevanceLabel{Pair: model.NewRelevancePair("a", fmt.Sprintf("p%04d", i)), Score: score, Kind: model.LabelPositive})
	}
	return labels
}

func highScores(n int) []uint8 {
	out := make([]uint8, n)
	for i := range out {
		out[i] = 200
	}
	return out
}

func TestCalibrateCosine(t *testing.T) {
	if floor := model.CalibrateCosine(cosineLabels(highScores(188)...)); floor.Valid {
		t.Fatalf("floor %+v: 188 positives cannot hold the recall bound", floor)
	}
	floor := model.CalibrateCosine(cosineLabels(append(highScores(188), 150)...))
	if !floor.Valid || floor.Floor != 150 || floor.Misses != 0 {
		t.Fatalf("floor %+v: 189 positives allow no miss, so the floor is the lowest positive", floor)
	}
	floor = model.CalibrateCosine(cosineLabels(append(highScores(278), 40, 90)...))
	if !floor.Valid || floor.Floor != 90 || floor.Misses != 1 {
		t.Fatalf("floor %+v: 280 positives allow one miss below the floor", floor)
	}
	negatives := append(cosineLabels(append(highScores(188), 150)...), model.RelevanceLabel{Pair: model.NewRelevancePair("a", "n"), Score: 5, Kind: model.LabelNegative})
	if got := model.CalibrateCosine(negatives); got.Floor != 150 {
		t.Fatal("negative labels should not move the floor")
	}
}

func TestPlaceByCosine_SetsAsideBelowTheFloorOnly(t *testing.T) {
	candidates := []model.GatherCandidate{{Entry: &model.Entry{ID: "low"}}, {Entry: &model.Entry{ID: "high"}}, {Entry: &model.Entry{ID: "unscored"}}}
	scores := map[string]uint8{"low": 10, "high": 150}
	kept, setAside := model.PlaceByCosine(candidates, scores, model.CosineFloor{Valid: true, Floor: 150})
	if len(kept) != 2 || kept[0].Entry.ID != "high" || len(setAside) != 1 || setAside[0] != "low" {
		t.Fatalf("kept %v, set aside %v: only a candidate below the floor goes", kept, setAside)
	}
	kept, setAside = model.PlaceByCosine(candidates, scores, model.CosineFloor{Floor: 150})
	if len(kept) != 3 || setAside != nil {
		t.Fatal("an invalid floor should set nothing aside")
	}
}

// The cosine stage's similarities reach the ledger only for pairs a reader
// labelled; its set-asides are never stored (5.8 item 11).
func TestCaptureRelevance_StoresCosineOnlyForLabelledPairs(t *testing.T) {
	judge := model.CosineJudge("counter/v1")
	var reading model.Reading
	reading.Fold(model.ReadPayload{Cosine: &model.PrefilterPayload{Judge: judge, Scores: map[string]uint8{"20260101-000000-s-tac-ref": 200, "20260101-000000-s-tac-off": 3}}})
	batch, err := model.CaptureRelevance("20261109-000000-d-tac-new", &reading, []string{"20260101-000000-s-tac-ref"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Scores) != 1 || batch.Scores[0].Score.Judge != judge || !batch.Scores[0].Pair.Has("20260101-000000-s-tac-ref") {
		t.Fatalf("scores %+v: only the referenced pair should keep its similarity", batch.Scores)
	}
}
