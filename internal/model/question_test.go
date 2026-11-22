package model_test

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

const questionSample = `Draft one entry with the user, dialogue first. Check its grounding against the graph before you write.

- Keep the summary under 280 characters so the listing stays short.
- Name every reference the draft leans on.

## Playback

The draft must reach the user whole, never as a diff. Ask the user to confirm it.`

// onlyKind draws from a source whose first pick lands on kind's place after
// the shuffle, by trying seeds until DrawQuestion returns that kind.
func drawKind(t *testing.T, text string, kind model.QuestionKind) model.TextQuestion {
	t.Helper()
	for seed := range uint64(500) {
		q, ok := model.DrawQuestion(text, rand.New(rand.NewPCG(seed, seed)), nil)
		if ok && q.Kind == kind {
			return q
		}
	}
	t.Fatalf("no seed drew a question of kind %s", kind)
	return model.TextQuestion{}
}

func TestDrawQuestionAnswersEachKindFromTheText(t *testing.T) {
	tests := []struct {
		kind  model.QuestionKind
		check func(q model.TextQuestion) bool
	}{
		{model.AskWordsAfter, func(q model.TextQuestion) bool {
			return strings.Contains(unpunctuated(questionSample), q.Phrase+" "+q.Answer)
		}},
		{model.AskWordsBefore, func(q model.TextQuestion) bool {
			return strings.Contains(unpunctuated(questionSample), q.Answer+" "+q.Phrase)
		}},
		{model.AskBetween, func(q model.TextQuestion) bool {
			return strings.Contains(questionSample, q.Phrase+" "+q.Answer+" "+q.Second)
		}},
		{model.AskNumberAfter, func(q model.TextQuestion) bool {
			return q.Answer == "280" && strings.Contains(questionSample, q.Phrase+" 280")
		}},
		{model.AskSentenceFirst, func(q model.TextQuestion) bool {
			sentence := sentenceHolding(q.Phrase)
			return strings.HasPrefix(strings.TrimPrefix(sentence, "- "), q.Answer)
		}},
		{model.AskSentenceLast, func(q model.TextQuestion) bool {
			sentence := strings.TrimSuffix(sentenceHolding(q.Phrase), ".")
			return strings.HasSuffix(sentence, q.Answer)
		}},
		{model.AskNextSentence, func(q model.TextQuestion) bool {
			sentence := sentenceHolding(q.Phrase)
			rest := strings.TrimPrefix(strings.TrimSpace(questionSample[strings.Index(questionSample, sentence)+len(sentence):]), "- ")
			return strings.HasPrefix(rest, q.Answer)
		}},
	}
	for _, tc := range tests {
		t.Run(string(tc.kind), func(t *testing.T) {
			q := drawKind(t, questionSample, tc.kind)
			if strings.Count(questionSample, q.Phrase) != 1 {
				t.Errorf("the phrase should occur once in the text, got %q", q.Phrase)
			}
			if !tc.check(q) {
				t.Errorf("the answer should be the words the question asks for, got %+v", q)
			}
			if !q.Answered(q.Answer) {
				t.Errorf("the expected answer should count as right, got %+v", q)
			}
		})
	}
}

// unpunctuated drops the commas and full stops between words, which an
// answer leaves out.
func unpunctuated(text string) string { return strings.NewReplacer(",", "", ".", "").Replace(text) }

// sentenceHolding returns the sentence of questionSample holding phrase.
func sentenceHolding(phrase string) string {
	for line := range strings.SplitSeq(questionSample, "\n") {
		for sentence := range strings.SplitAfterSeq(line, ". ") {
			if strings.Contains(sentence, phrase) {
				return strings.TrimSpace(sentence)
			}
		}
	}
	return ""
}

func TestDrawQuestionHonoursUsable(t *testing.T) {
	asked := map[string]bool{}
	rnd := rand.New(rand.NewPCG(1, 2))
	for range 20 {
		q, ok := model.DrawQuestion(questionSample, rnd, func(q model.TextQuestion) bool { return !asked[q.Key()] })
		if !ok {
			t.Fatal("the text should hold 20 distinct questions")
		}
		if asked[q.Key()] {
			t.Fatalf("a question rejected as asked should never be drawn, got %+v", q)
		}
		asked[q.Key()] = true
	}
	if _, ok := model.DrawQuestion(questionSample, rnd, func(model.TextQuestion) bool { return false }); ok {
		t.Error("no question should be drawn when none is usable")
	}
}

func TestDrawQuestionSkipsCodeAndRepeatedPhrases(t *testing.T) {
	text := "```\nalpha beta gamma delta epsilon zeta\n```\nsame words here. same words here."
	if q, ok := model.DrawQuestion(text, rand.New(rand.NewPCG(3, 4)), nil); ok {
		t.Errorf("a text whose only phrases are code or repeated should yield no question, got %+v", q)
	}
}

func TestTextQuestionAnswered(t *testing.T) {
	q := model.TextQuestion{Kind: model.AskWordsAfter, Phrase: "the draft must", Answer: "reach the"}
	number := model.TextQuestion{Kind: model.AskNumberAfter, Phrase: "the summary under", Answer: "18,000"}
	tests := []struct {
		name  string
		q     model.TextQuestion
		given string
		right bool
	}{
		{"exact", q, "reach the", true},
		{"case and quotes aside", q, `"Reach The."`, true},
		{"one word short", q, "reach", false},
		{"other words", q, "be whole", false},
		{"lost", q, "lost", false},
		{"empty", q, "", false},
		{"number with grouping", number, "18,000", true},
		{"number without grouping", number, "18000", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.q.Answered(tc.given); got != tc.right {
				t.Errorf("Answered should report %v for this answer", tc.right)
			}
		})
	}
}

func TestIsLost(t *testing.T) {
	for _, given := range []string{"lost", "LOST", ` "lost". `} {
		if !model.IsLost(given) {
			t.Errorf("%q should read as lost", given)
		}
	}
	if model.IsLost("lost words") {
		t.Error("an answer holding more than lost should not read as lost")
	}
}

func TestTextQuestionLineNamesTextAndLost(t *testing.T) {
	q := model.TextQuestion{Kind: model.AskWordsAfter, Phrase: "the draft must", Answer: "reach the"}
	line := q.Line("lane playback")
	if line != `q lane playback: 2 words after "the draft must"? | lost` {
		t.Errorf("the line should name the text, the question and lost, got %q", line)
	}
	for _, kind := range model.QuestionKinds {
		ask := model.TextQuestion{Kind: kind, Phrase: "a b c", Second: "d e f"}.Ask()
		if strings.Contains(ask, "paragraph") || strings.Contains(ask, "word number") {
			t.Errorf("no question should count paragraphs or words, got %q", ask)
		}
	}
}

func TestSessionNeutralComparesCopiesAcrossSessions(t *testing.T) {
	const (
		text   = "Resume it with resume_session HANDLE when its topic returns. Its parked moves keep their state and HANDLE stays valid."
		first  = "s_20261122-100000-aaaa"
		second = "s_20261122-110000-bbbb"
	)
	a := model.SessionNeutral(strings.ReplaceAll(text, "HANDLE", first), first)
	b := model.SessionNeutral(strings.ReplaceAll(text, "HANDLE", second), second)
	if a != b {
		t.Fatalf("copies differing only in their session handle should compare as one text, got %q and %q", a, b)
	}
	for seed := range uint64(300) {
		q, ok := model.DrawQuestion(a, rand.New(rand.NewPCG(seed, seed)), nil)
		if !ok {
			t.Fatal("the neutral text should still yield questions")
		}
		for _, part := range []string{q.Phrase, q.Second, q.Answer} {
			if strings.Contains(part, "session§") || strings.Contains(part, "handle") {
				t.Fatalf("no question should ask about the session handle, got %+v", q)
			}
		}
	}
}
