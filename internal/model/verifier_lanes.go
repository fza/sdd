package model

import "slices"

// VerifierDraft is what the deep verifier's lane split reads of a draft
// (20261109-004506-d-cpt-udz, D29, D33, D36): its kind, its topics and the
// entries it declares as refs, supersedes and closes.
type VerifierDraft struct {
	Kind    Kind
	Topics  []TopicPath
	Targets []string
}

// VerifierStructural returns the entries the verifier inspects in full
// blocks whatever they score: the declared targets, the entries read in
// full, the entries the drafter gave a verdict, every entry one ref, close
// or supersede away from a target in either direction, and the listed
// entries of the draft's kind on its narrowest topics.
func VerifierStructural(g *Graph, draft VerifierDraft, listed, full, verdicts []string) map[string]bool {
	structural := map[string]bool{}
	for _, ids := range [][]string{draft.Targets, full, verdicts} {
		for _, id := range ids {
			structural[id] = true
		}
	}
	for _, id := range draft.Targets {
		if target := g.ByID[id]; target != nil {
			for _, ref := range target.Refs {
				structural[ref.ID] = true
			}
			for _, other := range append(slices.Clone(target.Closes), target.Supersedes...) {
				structural[other] = true
			}
		}
		for _, inbound := range [][]string{g.RefsTo[id], g.ClosedBy[id], g.SupersededBy[id]} {
			for _, other := range inbound {
				structural[other] = true
			}
		}
	}
	narrowest := 0
	for _, topic := range draft.Topics {
		narrowest = max(narrowest, len(topic.Components))
	}
	for _, id := range listed {
		e := g.ByID[id]
		if e == nil || e.Kind != draft.Kind || draft.Kind == "" {
			continue
		}
		for _, topic := range draft.Topics {
			if len(topic.Components) == narrowest && slices.ContainsFunc(g.EffectiveTopics(e), func(t TopicPath) bool { return t.HasPrefix(topic) }) {
				structural[id] = true
				break
			}
		}
	}
	return structural
}

// VerifierLanes is the verifier's reading split in two: Inspect served as
// blocks, in listed order with the audit samples placed blind among them;
// Hidden served one line each, in listed order. Audit names the samples.
type VerifierLanes struct {
	Inspect []string
	Hidden  []string
	Audit   []string
}

// SplitVerifierLanes splits the listed entries by the prefilter's scores.
// While the thresholds are invalid every entry is inspected, as before any
// calibration. Once valid, an entry is hidden only when it is not
// structural, holds a score, and scores below threshold; AuditSample of the
// hidden ones return to the inspect lane blind (D36).
func SplitVerifierLanes(listed []string, structural map[string]bool, scores map[string]uint8, thresholds RelevanceThresholds, threshold float64, seed string) VerifierLanes {
	if !thresholds.Valid {
		return VerifierLanes{Inspect: slices.Clone(listed)}
	}
	var inspect, below []string
	for _, id := range listed {
		score, scored := scores[id]
		if structural[id] || !scored || ScoreValue(score) >= threshold {
			inspect = append(inspect, id)
		} else {
			below = append(below, id)
		}
	}
	audit, _ := AuditSample(below, seed)
	lanes := VerifierLanes{Inspect: inspect, Audit: audit}
	for _, id := range below {
		if !slices.Contains(audit, id) {
			lanes.Hidden = append(lanes.Hidden, id)
		}
	}
	for _, id := range audit {
		lanes.Inspect = slices.Insert(lanes.Inspect, blindPosition(seed, id, len(lanes.Inspect)), id)
	}
	return lanes
}
