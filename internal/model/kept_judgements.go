package model

import "slices"

// SecondLookTop is how many entries of a check's merged order a kept low
// judgement must be among to be judged again, and how many entries nearest
// to the draft the rule by its meaning compares (20261121-233807-d-tac-4iw).
const SecondLookTop = 20

// DraftMark is the draft a check read against: its topics, the entries it
// references and, where a search by meaning ran, the entries nearest to it.
// Judged marks the draft as judged: the capture's first draft, or one the
// draft moved to (DraftMoved).
type DraftMark struct {
	Topics     []string `json:"topics,omitempty"`
	References []string `json:"references,omitempty"`
	Nearest    []string `json:"nearest,omitempty"`
	Judged     bool     `json:"judged,omitempty"`
}

// LastDraft returns the draft the reading's latest check read against, nil
// before one did.
func (r *Reading) LastDraft() *DraftMark {
	if r == nil {
		return nil
	}
	return r.lastDraft
}

// JudgedDraft returns the draft as judged, nil before a check marked one.
func (r *Reading) JudgedDraft() *DraftMark {
	if r == nil {
		return nil
	}
	return r.judgedDraft
}

func (r *Reading) foldDraft(mark *DraftMark) {
	if mark == nil {
		return
	}
	r.lastDraft = mark
	if mark.Judged || r.judgedDraft == nil {
		r.judgedDraft = mark
	}
}

// Kept returns every judgement judge gave inside the reading, each against
// the draft it was given for: a judgement stays with the capture when the
// draft changes.
func (r *Reading) Kept(judge string) map[string]uint8 {
	if r == nil || r.scores[CanonicalJudge(judge)] == nil {
		return nil
	}
	return r.scores[CanonicalJudge(judge)].scores
}

// Stale returns the judgements judge gave inside the reading against another
// draft than the one subject keys (RelevanceSubjectHash): those a second look
// may judge again.
func (r *Reading) Stale(judge, subject string) map[string]uint8 {
	if r == nil || r.scores[CanonicalJudge(judge)] == nil {
		return nil
	}
	held := r.scores[CanonicalJudge(judge)]
	stale := map[string]uint8{}
	for id, score := range held.scores {
		if held.against[id] != subject {
			stale[id] = score
		}
	}
	return stale
}

// DraftMoved reports whether the draft moved from the draft as judged: fewer
// than three quarters of the entries nearest to it are among those nearest to the
// draft now. Without a list of nearest entries nothing moves.
func DraftMoved(judged *DraftMark, nearest []string) bool {
	if judged == nil || len(judged.Nearest) == 0 {
		return false
	}
	kept := 0
	for _, id := range judged.Nearest {
		if slices.Contains(nearest, id) {
			kept++
		}
	}
	return 4*kept < 3*len(judged.Nearest)
}

// JudgedBearing reports whether a judgement places its entry as likely
// bearing: at or above the relevant threshold where thresholds hold, else at
// or above FallbackMarkScore.
func JudgedBearing(score uint8, thresholds RelevanceThresholds) bool {
	if thresholds.Valid {
		return ScoreValue(score) >= thresholds.Relevant
	}
	return ScoreValue(score) >= FallbackMarkScore
}

// SecondLook is what decides which kept low judgements a check judges
// again: the judgements given against an earlier draft (Reading.Stale), the
// merged order of its candidates, the draft the previous check read
// against, the draft now, and whether the draft moved.
type SecondLook struct {
	Graph      *Graph
	Live       []*Entry
	Kept       map[string]uint8
	Thresholds RelevanceThresholds
	Order      []string
	Previous   *DraftMark
	Current    DraftMark
	Moved      bool
}

// Due returns the entries with a kept low judgement due to be judged again,
// sorted: every one when the draft moved, else those among the first
// SecondLookTop of the merged order, those under a topic the draft gained,
// and those one reference step from a reference it gained. A judgement that
// places its entry as likely bearing is never judged again.
func (s SecondLook) Due() []string {
	low := map[string]bool{}
	for id, score := range s.Kept {
		if !JudgedBearing(score, s.Thresholds) {
			low[id] = true
		}
	}
	due := map[string]bool{}
	if s.Moved {
		return sortedKeys(low)
	}
	for _, id := range s.Order[:min(SecondLookTop, len(s.Order))] {
		if low[id] {
			due[id] = true
		}
	}
	if s.Previous != nil {
		for _, label := range s.Current.Topics {
			if slices.Contains(s.Previous.Topics, label) {
				continue
			}
			path, err := ParseTopicPath(label)
			if err != nil {
				continue
			}
			for _, e := range TopicListing(s.Graph, s.Live, path) {
				if low[e.ID] {
					due[e.ID] = true
				}
			}
		}
		var gained []string
		for _, id := range s.Current.References {
			if !slices.Contains(s.Previous.References, id) {
				gained = append(gained, id)
			}
		}
		for id := range withinRefHops(s.Graph, gained, 1) {
			if low[id] {
				due[id] = true
			}
		}
	}
	return sortedKeys(due)
}
