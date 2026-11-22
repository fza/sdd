package relevance

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"slices"
)

// ScoreWithFar scores request as s.Score does, each call carrying far among
// its candidates: texts known to be far from the subject, whose scores
// measure what the judge gives an unrelated text in that very call. The far
// candidates stand at positions drawn from the subject and their IDs, so a
// repeated request reads alike. The result holds the candidates' scores
// alone, with the line of each one's call in Far. A Halving scorer carries far into every
// call it splits the request into; it must be the outermost scorer for
// that. Without far the call is s.Score.
func ScoreWithFar(ctx context.Context, s Scorer, request Request, far []Candidate) (Result, error) {
	if len(far) == 0 {
		return s.Score(ctx, request)
	}
	if splitting, ok := s.(halving); ok {
		return splitting.score(ctx, request, far)
	}
	call, isFar := withFar(request, far)
	result, err := s.Score(ctx, call)
	if err != nil {
		return Result{}, err
	}
	err = CheckResult(call, result)
	if err != nil {
		return Result{}, err
	}
	return withoutFar(result, isFar), nil
}

// PlanWithFar is Plan for calls that each carry far beside their
// candidates: far takes its room of MaxBatch and its tokens of
// MaxInputTokens in every call. ok is false where no call holds far and one
// candidate more, or a far candidate passes MaxPairTokens beside the subject.
func PlanWithFar(scorer Scorer, subject string, candidates, far []Candidate) (batches [][]int, oversize []int, ok bool) {
	maxBatch, maxTokens, maxPair := scorer.MaxBatch(), scorer.MaxInputTokens(), scorer.MaxPairTokens()
	subjectTokens := EstimateTokens(subject)
	farTokens := 0
	for _, candidate := range far {
		cost := EstimateTokens(candidate.Text)
		if maxPair > 0 && subjectTokens+cost > maxPair {
			return nil, nil, false
		}
		farTokens += cost
	}
	if maxBatch > 0 && maxBatch <= len(far) || maxTokens > 0 && subjectTokens+farTokens > maxTokens {
		return nil, nil, false
	}
	batches, oversize = plan(scorer, subject, candidates, len(far), farTokens)
	return batches, oversize, true
}

// withFar returns the call of request with far among its candidates, and
// which candidates of the call are far.
func withFar(request Request, far []Candidate) (Request, []bool) {
	mixed := slices.Clone(request.Candidates)
	isFar := make([]bool, len(mixed))
	for _, candidate := range far {
		at := farPosition(request.Subject, candidate.ID, len(mixed))
		mixed = slices.Insert(mixed, at, candidate)
		isFar = slices.Insert(isFar, at, true)
	}
	return Request{Subject: request.Subject, Candidates: mixed}, isFar
}

func farPosition(subject, id string, n int) int {
	sum := sha256.Sum256([]byte(subject + "\nfar\n" + id))
	return int(binary.BigEndian.Uint64(sum[:8]) % uint64(n+1))
}

// withoutFar takes the far scores out of a call's result and sets the line
// they draw for every candidate left.
func withoutFar(result Result, isFar []bool) Result {
	out := Result{Identity: result.Identity, Usage: result.Usage}
	var far []float64
	for i, score := range result.Scores {
		if isFar[i] {
			far = append(far, score)
			continue
		}
		out.Scores = append(out.Scores, score)
	}
	slices.Sort(far)
	out.Far = slices.Repeat([]FarLine{{Lowest: far[0], Median: far[(len(far)-1)/2]}}, len(out.Scores))
	return out
}
