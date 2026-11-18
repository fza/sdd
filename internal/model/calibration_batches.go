package model

import (
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"slices"
	"strings"
)

// CalibrationBatch is one subject's unscored labelled candidates a
// background calibration sends together; Positive is the weight of the
// positive labels among them.
type CalibrationBatch struct {
	Subject    string
	Candidates []string
	Positive   float64
}

// CalibrationBatches plans a background calibration of judge. Only the
// positive labels move a recall stage, but a chat judge scores a candidate
// against the others in its call, so each subject (the later entry of a
// pair) holding an unscored positive label sends its unscored positive
// candidates with a sample of its other unscored labelled candidates, up to
// size in all. The sample is drawn by a hash of subject and candidate, so
// every plan draws the same. Subjects with the most positive weight go
// first, the latest first among equals.
func (l *RelevanceLedger) CalibrationBatches(judge string, size int) []CalibrationBatch {
	bySubject := map[string]*CalibrationBatch{}
	others := map[string][]string{}
	for _, pair := range l.Unscored(judge) {
		verdict := l.readers[pair]
		batch := bySubject[pair.B]
		if batch == nil {
			batch = &CalibrationBatch{Subject: pair.B}
			bySubject[pair.B] = batch
		}
		if LabelKind(verdict) == LabelPositive {
			batch.Candidates = append(batch.Candidates, pair.A)
			batch.Positive += LabelWeight(verdict)
			continue
		}
		others[pair.B] = append(others[pair.B], pair.A)
	}
	var batches []CalibrationBatch
	for subject, batch := range bySubject {
		if batch.Positive == 0 {
			continue
		}
		sample := others[subject]
		slices.SortFunc(sample, func(x, y string) int { return cmp.Compare(sampleRank(subject, x), sampleRank(subject, y)) })
		if room := size - len(batch.Candidates); room < len(sample) {
			sample = sample[:max(room, 0)]
		}
		batch.Candidates = append(batch.Candidates, sample...)
		slices.Sort(batch.Candidates)
		batches = append(batches, *batch)
	}
	slices.SortFunc(batches, func(x, y CalibrationBatch) int {
		if c := cmp.Compare(y.Positive, x.Positive); c != 0 {
			return c
		}
		return strings.Compare(y.Subject, x.Subject)
	})
	return batches
}

func sampleRank(subject, candidate string) uint64 {
	sum := sha256.Sum256([]byte(subject + "\ncalibration\n" + candidate))
	return binary.BigEndian.Uint64(sum[:8])
}
