package model

// LabelTally counts the ledger's labelled pairs, whatever judge scored them:
// Labelled the pairs a reader record makes a positive or negative label,
// Referenced those referenced, Implicit the implicit dismissals and Flipped
// those a later entry flipped to bearing, Coded the other negatives, set
// aside with a code.
type LabelTally struct {
	Labelled, Referenced, Implicit, Flipped, Coded int
}

// Tally counts the ledger's labels, the live ones alone with excludeReplay.
func (l *RelevanceLedger) Tally(excludeReplay bool) LabelTally {
	var tally LabelTally
	for _, verdict := range l.readers {
		if excludeReplay && verdict.Source != SourceLive {
			continue
		}
		kind := LabelKind(verdict)
		if kind == LabelPositive || kind == LabelNegative {
			tally.Labelled++
		}
		switch {
		case verdict.Code == ReaderReferenced:
			tally.Referenced++
		case verdict.Code == ReaderImplicit:
			tally.Implicit++
			if verdict.Flipped {
				tally.Flipped++
			}
		case kind == LabelNegative:
			tally.Coded++
		}
	}
	return tally
}

// RecallFloorPositives is the weight of positive labels at which a judge
// that misses none reaches RecallFloor: the Wilson bound of n successes of n
// is n/(n+z²).
func RecallFloorPositives() float64 {
	return RecallFloor / (1 - RecallFloor) * wilsonZ * wilsonZ
}
