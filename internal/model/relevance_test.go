package model_test

import (
	"reflect"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func readerVerdicts() []model.ReaderVerdict {
	var all []model.ReaderVerdict
	for code := model.ReaderNone; code <= model.ReaderReferenced; code++ {
		for _, reason := range []uint8{0, 1, 4, 9} {
			for _, audit := range []bool{false, true} {
				for _, relations := range []uint8{0, model.RelationContradicts, model.RelationDuplicates | model.RelationOutdates} {
					all = append(all, model.ReaderVerdict{Code: code, Reason: reason, Audit: audit, Relations: relations})
				}
			}
		}
	}
	return all
}

func TestReaderVerdictJoin_IsALattice(t *testing.T) {
	all := readerVerdicts()
	for _, a := range all {
		if a.Join(a) != a {
			t.Fatalf("join of %+v with itself changed it", a)
		}
		for _, b := range all {
			if a.Join(b) != b.Join(a) {
				t.Fatalf("join of %+v and %+v depends on the order", a, b)
			}
			for _, c := range all {
				if a.Join(b).Join(c) != a.Join(b.Join(c)) {
					t.Fatalf("join of %+v, %+v, %+v depends on the grouping", a, b, c)
				}
			}
		}
	}
}

func TestReaderVerdictJoin_TheDeeperAnswerWins(t *testing.T) {
	dismissed := model.ReaderVerdict{Code: model.ReaderDismissed, Reason: 1, Relations: model.RelationContradicts}
	referenced := model.ReaderVerdict{Code: model.ReaderReferenced, Audit: true}
	got := dismissed.Join(referenced)
	if got.Code != model.ReaderReferenced || got.Reason != 0 || !got.Audit || got.Relations != model.RelationContradicts {
		t.Fatalf("join = %+v: want the referenced code without the dismissal's reason, and both flags kept", got)
	}
}

func TestVerdictReasonByte_RoundTripsEveryCode(t *testing.T) {
	seen := map[uint8]bool{}
	for _, code := range model.GroundingVerdictCodes() {
		b, err := model.VerdictReasonByte(code)
		if err != nil {
			t.Fatal(err)
		}
		if b == 0 || seen[b] {
			t.Fatalf("code %s takes byte %d: each code needs its own non-zero byte", code, b)
		}
		seen[b] = true
		if model.VerdictReasonCode(b) != code {
			t.Fatalf("byte of %s names %q", code, model.VerdictReasonCode(b))
		}
		parsed, err := model.ParseVerdictReason(model.VerdictReasonCode(b))
		if err != nil || parsed != b {
			t.Fatalf("parsing %s = %d, %v", code, parsed, err)
		}
	}
	if seen[5] {
		t.Fatal("byte 5, the slot of the removed superseded code, is taken")
	}
	if got := model.VerdictReasonCode(42); got != "reason-42" {
		t.Fatalf("an unknown byte names %q: it should stay readable as its number", got)
	}
	if parsed, err := model.ParseVerdictReason("reason-42"); err != nil || parsed != 42 {
		t.Fatalf("parsing an unknown byte's name = %d, %v", parsed, err)
	}
	if _, err := model.VerdictReasonByte("superseded"); err == nil {
		t.Fatal("the removed superseded code is accepted")
	}
}

const (
	subject   = "20261109-120000-d-cpt-sub"
	candidate = "20261101-090000-s-tac-can"
	judge     = "systemone:jev-1.14.0"
)

func TestRelevanceLedgerLookup(t *testing.T) {
	pair := model.NewRelevancePair(subject, candidate)
	calibrated := model.RelevanceThresholds{Valid: true, SetAside: 0.2, Relevant: 0.6}
	cases := []struct {
		name       string
		batch      model.RelevanceBatch
		thresholds model.RelevanceThresholds
		want       model.RelevanceDecision
	}{
		{"nothing stored", model.RelevanceBatch{}, calibrated, model.RelevanceUndecided},
		{"a reader reference decides", model.RelevanceBatch{Readers: []model.ReaderRecord{{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderReferenced}}}}, calibrated, model.RelevanceRelevant},
		{"a reader dismissal decides over a high score", model.RelevanceBatch{
			Readers: []model.ReaderRecord{{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderDismissed, Reason: 1}}},
			Scores:  []model.ScoreRecord{{Pair: pair, Score: model.FilterScore{Judge: judge, Score: 250}}},
		}, calibrated, model.RelevanceNotRelevant},
		{"a low score sets aside", model.RelevanceBatch{Scores: []model.ScoreRecord{{Pair: pair, Score: model.FilterScore{Judge: judge, Score: 10}}}}, calibrated, model.RelevanceNotRelevant},
		{"a high score is relevant", model.RelevanceBatch{Scores: []model.ScoreRecord{{Pair: pair, Score: model.FilterScore{Judge: judge, Score: 200}}}}, calibrated, model.RelevanceRelevant},
		{"a middle score stays to judge", model.RelevanceBatch{Scores: []model.ScoreRecord{{Pair: pair, Score: model.FilterScore{Judge: judge, Score: 100}}}}, calibrated, model.RelevanceUndecided},
		{"uncalibrated scores decide nothing", model.RelevanceBatch{Scores: []model.ScoreRecord{{Pair: pair, Score: model.FilterScore{Judge: judge, Score: 10}}}}, model.RelevanceThresholds{}, model.RelevanceUndecided},
		{"another judge's score decides nothing", model.RelevanceBatch{Scores: []model.ScoreRecord{{Pair: pair, Score: model.FilterScore{Judge: "generate:haiku", Score: 10}}}}, calibrated, model.RelevanceUndecided},
		{"a one-way score in this direction decides", model.RelevanceBatch{Scores: []model.ScoreRecord{{Pair: pair, Score: model.FilterScore{Judge: judge, Direction: model.DirectionFrom(pair, subject), Score: 10}}}}, calibrated, model.RelevanceNotRelevant},
		{"a reverse one-way low score decides nothing", model.RelevanceBatch{Scores: []model.ScoreRecord{{Pair: pair, Score: model.FilterScore{Judge: judge, Direction: model.DirectionFrom(pair, candidate), Score: 10}}}}, calibrated, model.RelevanceUndecided},
		{"a reverse one-way high score is relevant", model.RelevanceBatch{Scores: []model.ScoreRecord{{Pair: pair, Score: model.FilterScore{Judge: judge, Direction: model.DirectionFrom(pair, candidate), Score: 200}}}}, calibrated, model.RelevanceRelevant},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ledger := model.NewRelevanceLedger(tc.batch)
			if got := ledger.Lookup(subject, candidate, judge, tc.thresholds); got != tc.want {
				t.Fatalf("Lookup = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRelevanceLedger_FoldsToTheSameBatchInAnyOrder(t *testing.T) {
	pair := model.NewRelevancePair(subject, candidate)
	a := model.RelevanceBatch{Readers: []model.ReaderRecord{{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderDismissed, Reason: 2}}}, Scores: []model.ScoreRecord{{Pair: pair, Score: model.FilterScore{Judge: judge, Score: 3}}}}
	b := model.RelevanceBatch{Readers: []model.ReaderRecord{{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderOpened, Reason: 1}}}, Scores: []model.ScoreRecord{{Pair: pair, Score: model.FilterScore{Judge: judge, Score: 9}}}}
	forward := model.NewRelevanceLedger(a, b).Batch()
	backward := model.NewRelevanceLedger(b, a, b).Batch()
	if !reflect.DeepEqual(forward, backward) {
		t.Fatalf("ledgers differ:\n%+v\n%+v", forward, backward)
	}
	if forward.Readers[0].Verdict.Code != model.ReaderOpened || forward.Scores[0].Score.Score != 9 {
		t.Fatalf("folded batch = %+v: want the opened answer and the higher score", forward)
	}
}
