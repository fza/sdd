package model_test

import (
	"reflect"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func readerVerdicts() []model.ReaderVerdict {
	var all []model.ReaderVerdict
	for _, code := range []model.ReaderCode{model.ReaderNone, model.ReaderImplicit, model.ReaderDismissed, model.ReaderOpened, model.ReaderReferenced} {
		for _, reason := range []uint8{0, 1, 9} {
			for _, flags := range [][2]bool{{false, false}, {true, false}, {false, true}} {
				for _, relations := range []uint8{0, model.RelationContradicts, model.RelationDuplicates | model.RelationOutdates} {
					for _, source := range []model.ReaderSource{model.SourceLive, model.SourceReplay} {
						all = append(all, model.ReaderVerdict{Code: code, Reason: reason, Audit: flags[0], Flipped: flags[1], Relations: relations, Source: source})
					}
				}
			}
		}
	}
	return all
}

// The implicit dismissal sits below every answer the reader gave: any
// explicit record of the pair replaces it, and a flip keeps its code.
func TestReaderVerdictJoin_TheImplicitDismissalRanksLowest(t *testing.T) {
	implicit := model.ReaderVerdict{Code: model.ReaderImplicit, Audit: true}
	for _, code := range []model.ReaderCode{model.ReaderDismissed, model.ReaderOpened, model.ReaderReferenced} {
		if got := implicit.Join(model.ReaderVerdict{Code: code, Reason: 1}); got.Code != code || !got.Audit {
			t.Errorf("join with %s = %+v: the explicit answer should win and the audit bit stay", code, got)
		}
	}
	if got := implicit.Join(model.ReaderVerdict{}); got.Code != model.ReaderImplicit {
		t.Errorf("join with no answer = %+v: the implicit dismissal should stay", got)
	}
	flipped := implicit.Join(model.ReaderVerdict{Code: model.ReaderImplicit, Flipped: true})
	if flipped.Code != model.ReaderImplicit || !flipped.Flipped || !flipped.Audit {
		t.Errorf("join with a flip = %+v: the implicit record should carry the flip", flipped)
	}
	for _, code := range []model.ReaderCode{model.ReaderNone, model.ReaderImplicit, model.ReaderDismissed, model.ReaderOpened, model.ReaderReferenced} {
		parsed, err := model.ParseReaderCode(code.String())
		if err != nil || parsed != code {
			t.Errorf("parsing %s = %v, %v", code, parsed, err)
		}
	}
}

// A flip pairs entries the implicit dismissal left apart when a later entry
// references the subject and the candidate both.
func TestPairFlips_FlipsImplicitDismissalsALaterEntryJoins(t *testing.T) {
	const (
		s     = "20261101-120000-d-tac-sub"
		c     = "20261102-120000-s-tac-can"
		other = "20261103-120000-s-tac-oth"
		dis   = "20261104-120000-s-tac-dis"
	)
	topicOnly, _ := model.VerdictReasonByte(model.VerdictTopicOnly)
	ledger := model.NewRelevanceLedger(model.RelevanceBatch{Readers: []model.ReaderRecord{
		{Pair: model.NewRelevancePair(s, c), Verdict: model.ReaderVerdict{Code: model.ReaderImplicit, Source: model.SourceReplay}},
		{Pair: model.NewRelevancePair(s, dis), Verdict: model.ReaderVerdict{Code: model.ReaderDismissed, Reason: topicOnly}},
		{Pair: model.NewRelevancePair(other, s), Verdict: model.ReaderVerdict{Code: model.ReaderImplicit, Flipped: true}},
	}})
	got := model.PairFlips(ledger, []string{c, s, dis, other, s, ""})
	want := []model.ReaderRecord{{Pair: model.NewRelevancePair(s, c), Verdict: model.ReaderVerdict{Code: model.ReaderImplicit, Flipped: true, Source: model.SourceReplay}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("flips %+v\nwant %+v: only the unflipped implicit pair should flip, once", got, want)
	}
	ledger.Add(model.RelevanceBatch{Readers: got})
	if got := model.PairFlips(ledger, []string{s, c}); len(got) != 0 {
		t.Fatalf("flips %+v: a flipped pair should not flip again", got)
	}
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
		{"an implicit dismissal leaves the pair to the score", model.RelevanceBatch{
			Readers: []model.ReaderRecord{{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderImplicit}}},
			Scores:  []model.ScoreRecord{{Pair: pair, Score: model.FilterScore{Judge: judge, Score: 100}}},
		}, calibrated, model.RelevanceUndecided},
		{"a flipped implicit dismissal is relevant", model.RelevanceBatch{
			Readers: []model.ReaderRecord{{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderImplicit, Flipped: true}}},
			Scores:  []model.ScoreRecord{{Pair: pair, Score: model.FilterScore{Judge: judge, Score: 10}}},
		}, calibrated, model.RelevanceRelevant},
		{"a low score sets aside", model.RelevanceBatch{Scores: []model.ScoreRecord{{Pair: pair, Score: model.FilterScore{Judge: judge, Score: 10}}}}, calibrated, model.RelevanceNotRelevant},
		{"a score below both thresholds is set aside", model.RelevanceBatch{Scores: []model.ScoreRecord{{Pair: pair, Score: model.FilterScore{Judge: judge, Score: 10}}}}, model.RelevanceThresholds{Valid: true, SetAside: 0.2, Relevant: 0.012}, model.RelevanceNotRelevant},
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

func TestReaderVerdictJoin_ALiveRecordMakesThePairLive(t *testing.T) {
	replayed := model.ReaderVerdict{Code: model.ReaderReferenced, Source: model.SourceReplay}
	cases := []struct {
		name  string
		other model.ReaderVerdict
		want  model.ReaderSource
	}{
		{"live dismissal", model.ReaderVerdict{Code: model.ReaderDismissed, Reason: 1}, model.SourceLive},
		{"live reference", model.ReaderVerdict{Code: model.ReaderReferenced}, model.SourceLive},
		{"replayed reference", replayed, model.SourceReplay},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := replayed.Join(c.other).Source; got != c.want {
				t.Fatalf("source %s: a pair should stay replay only until a live record joins it", got)
			}
		})
	}
}

func TestParseReaderSource_RoundTripsAndReadsEmptyAsLive(t *testing.T) {
	for _, source := range []model.ReaderSource{model.SourceLive, model.SourceReplay} {
		got, err := model.ParseReaderSource(source.String())
		if err != nil || got != source {
			t.Fatalf("ParseReaderSource(%q) = %v, %v", source, got, err)
		}
	}
	if got, err := model.ParseReaderSource(""); err != nil || got != model.SourceLive {
		t.Fatalf("an absent source should read as live, got %v, %v", got, err)
	}
	if _, err := model.ParseReaderSource("seeded"); err == nil {
		t.Fatal("an unknown source should be refused")
	}
}

func TestReaderVerdictJoin_AnOpeningLeavesTheReadersLabel(t *testing.T) {
	dismissed := model.ReaderVerdict{Code: model.ReaderDismissed, Reason: 1}
	got := dismissed.Join(model.ReaderVerdict{OpenedHidden: true})
	if got.Code != model.ReaderDismissed || got.Reason != 1 || !got.OpenedHidden {
		t.Fatalf("join = %+v: an opening should keep the dismissal and add its flag", got)
	}
}

// A replaced score stands even below the old one, in either direction; the
// judge's other pairs and other judges keep theirs.
func TestRelevanceLedger_ReplaceScoresReplacesTheJudgesScoresOfItsPairs(t *testing.T) {
	const subject = "20260601-000000-d-tac-sub"
	replaced := model.NewRelevancePair("20260101-000000-s-tac-one", subject)
	kept := model.NewRelevancePair("20260101-000001-s-tac-two", subject)
	ledger := model.NewRelevanceLedger(model.RelevanceBatch{Scores: []model.ScoreRecord{
		{Pair: replaced, Score: model.FilterScore{Judge: "j", Direction: model.DirectionFrom(replaced, subject), Score: 200}},
		{Pair: replaced, Score: model.FilterScore{Judge: "k", Score: 100}},
		{Pair: kept, Score: model.FilterScore{Judge: "j", Score: 180}},
	}})
	ledger.ReplaceScores(model.RelevanceBatch{Scores: []model.ScoreRecord{{Pair: replaced, Score: model.FilterScore{Judge: "j", Score: 50}}}})
	for _, tc := range []struct {
		pair  model.RelevancePair
		judge string
		want  uint8
	}{{replaced, "j", 50}, {replaced, "k", 100}, {kept, "j", 180}} {
		if got, ok := ledger.Score(tc.pair, subject, tc.judge); !ok || got != tc.want {
			t.Errorf("%s score of %v = %d, %v; want %d", tc.judge, tc.pair, got, ok, tc.want)
		}
	}
	if got := len(ledger.Batch().Scores); got != 3 {
		t.Errorf("%d scores: the judge's old one-way score should be gone", got)
	}
}

// The scorable pairs are the labelled ones a score calibrates, scored or not.
func TestRelevanceLedger_ScorableListsThePositiveAndNegativeLabels(t *testing.T) {
	const subject = "20260601-000000-d-tac-sub"
	positive := model.NewRelevancePair("20260101-000000-s-tac-one", subject)
	negative := model.NewRelevancePair("20260101-000001-s-tac-two", subject)
	covered := model.NewRelevancePair("20260101-000002-s-tac-thr", subject)
	ledger := model.NewRelevanceLedger(model.RelevanceBatch{
		Readers: []model.ReaderRecord{
			{Pair: positive, Verdict: model.ReaderVerdict{Code: model.ReaderReferenced}},
			{Pair: negative, Verdict: model.ReaderVerdict{Code: model.ReaderImplicit}},
			{Pair: covered, Verdict: model.ReaderVerdict{Code: model.ReaderOpened}},
		},
		Scores: []model.ScoreRecord{{Pair: positive, Score: model.FilterScore{Judge: "j", Score: 200}}},
	})
	if got := ledger.Scorable(); !reflect.DeepEqual(got, []model.RelevancePair{positive, negative}) {
		t.Errorf("scorable = %v, want the positive and the negative label", got)
	}
}
