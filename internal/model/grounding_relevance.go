package model

import (
	"maps"
	"slices"
)

// CaptureRelevance returns the records a published capture adds to
// the relevance ledger, keyed to the entry it wrote (5.7): every ref,
// supersede and close is referenced; every verdict sets its entries aside
// with its code, as opened when the reading served them in full; every
// prefilter score the reading holds is kept with its judge.
func CaptureRelevance(entryID string, reading *Reading, answered []string, verdicts []GroundingVerdict) (RelevanceBatch, error) {
	var batch RelevanceBatch
	if reading == nil {
		return batch, nil
	}
	seen := map[string]bool{}
	for _, id := range answered {
		if id == "" || id == entryID || seen[id] {
			continue
		}
		seen[id] = true
		batch.Readers = append(batch.Readers, ReaderRecord{Pair: NewRelevancePair(entryID, id), Verdict: ReaderVerdict{Code: ReaderReferenced}})
	}
	for _, verdict := range verdicts {
		reason, err := VerdictReasonByte(verdict.Code)
		if err != nil {
			return RelevanceBatch{}, err
		}
		for _, id := range verdict.IDs {
			if id == "" || id == entryID || seen[id] {
				continue
			}
			seen[id] = true
			code := ReaderDismissed
			if reading.full[id] {
				code = ReaderOpened
			}
			batch.Readers = append(batch.Readers, ReaderRecord{Pair: NewRelevancePair(entryID, id), Verdict: ReaderVerdict{Code: code, Reason: reason}})
		}
	}
	for _, judge := range slices.Sorted(maps.Keys(reading.scores)) {
		held := reading.scores[judge]
		for _, id := range slices.Sorted(maps.Keys(held.scores)) {
			pair := NewRelevancePair(entryID, id)
			direction := DirectionBoth
			if held.forward {
				direction = DirectionFrom(pair, entryID)
			}
			batch.Scores = append(batch.Scores, ScoreRecord{Pair: pair, Score: FilterScore{Judge: judge, Direction: direction, Score: held.scores[id]}})
		}
	}
	return batch, nil
}

// LedgerSplit is what the relevance ledger decides of a reading's
// candidates against one subject.
type LedgerSplit struct {
	// Undecided stay to judge, in their order.
	Undecided []GatherCandidate
	// Relevant are decided relevant and served in full.
	Relevant []GatherCandidate
	// NotRelevant are decided not relevant and set aside, sorted by ID.
	NotRelevant []string
}

// SplitByLedger sorts candidates by what the ledger decides of each against
// subject: a stored verdict answers a repeat pair without a new judgment.
func SplitByLedger(candidates []GatherCandidate, subject string, ledger *RelevanceLedger, judge string, thresholds RelevanceThresholds) LedgerSplit {
	var split LedgerSplit
	for _, candidate := range candidates {
		if ledger == nil || subject == "" {
			split.Undecided = append(split.Undecided, candidate)
			continue
		}
		switch ledger.Lookup(subject, candidate.Entry.ID, judge, thresholds) {
		case RelevanceRelevant:
			split.Relevant = append(split.Relevant, candidate)
		case RelevanceNotRelevant:
			split.NotRelevant = append(split.NotRelevant, candidate.Entry.ID)
		default:
			split.Undecided = append(split.Undecided, candidate)
		}
	}
	slices.Sort(split.NotRelevant)
	return split
}
