package model

import (
	"slices"
	"strings"
)

// The cosine stage (20261109-004506-d-cpt-udz, 5.8 item 11): the vector
// similarity of the draft to each candidate, the score the semantic search
// ranks by, sets a candidate aside below a floor calibrated on the labelled
// pairs' similarities. Its set-asides are recomputable and never stored; the
// ledger keeps a similarity only for a pair a reader labelled.

// CosineJudgePrefix starts the judge name the cosine stage scores under; the
// rest is the embedding fingerprint, so a new vector space calibrates anew.
const CosineJudgePrefix = "cosine:"

// CosineJudge is the cosine stage's judge over the embedding space
// fingerprint names.
func CosineJudge(fingerprint string) string { return CosineJudgePrefix + fingerprint }

// IsCosineJudge reports whether judge is a cosine stage's.
func IsCosineJudge(judge string) bool { return strings.HasPrefix(judge, CosineJudgePrefix) }

// CosineFloor is the calibrated floor of the cosine stage: a candidate whose
// similarity byte lies below Floor is set aside. Invalid until enough
// positive labels hold the recall bound (D33).
type CosineFloor struct {
	Valid     bool
	Floor     uint8
	Positives int
	// Misses are the positives below the floor the recall bound allows.
	Misses int
}

// CalibrateCosine finds the highest floor whose recall over the positive
// labels keeps its Wilson lower bound (95%) at RecallFloor or above.
func CalibrateCosine(labels []RelevanceLabel) CosineFloor {
	var positives []uint8
	for _, label := range labels {
		if label.Kind == LabelPositive {
			positives = append(positives, label.Score)
		}
	}
	slices.Sort(positives)
	n := len(positives)
	floor := CosineFloor{Positives: n}
	for misses := n - 1; misses >= 0; misses-- {
		if WilsonLower(n-misses, n) >= RecallFloor {
			floor.Valid, floor.Floor, floor.Misses = true, positives[misses], misses
			return floor
		}
	}
	return floor
}

// PlaceByCosine sets aside the candidates whose similarity lies below a
// valid floor, in their order; a candidate without a similarity stays.
func PlaceByCosine(candidates []GatherCandidate, scores map[string]uint8, floor CosineFloor) ([]GatherCandidate, []string) {
	if !floor.Valid {
		return candidates, nil
	}
	var kept []GatherCandidate
	var setAside []string
	for _, candidate := range candidates {
		score, ok := scores[candidate.Entry.ID]
		if ok && score < floor.Floor {
			setAside = append(setAside, candidate.Entry.ID)
			continue
		}
		kept = append(kept, candidate)
	}
	return kept, setAside
}
