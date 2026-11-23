package model_test

import (
	"math/rand/v2"
	"regexp"
	"slices"
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

// splitSample holds what an agent and a parser cut differently: arrows,
// entry IDs, list items, code spans, abbreviations and signs before a
// number.
const splitSample = `Each entry's → upstream refs to the graph stay listed in the chain.
- 20260413-160358-d-stg-574 strategic directive → refs 20260410-210220-s-stg-gtu
- d-cpt-ah1 holds the planning contract for every layer split.
Keep the lead and trail sentences ≤ ~15 words, e.g. the summary line of each one.
Run ` + "`go test every model package here`" + ` before the commit lands, etc. Then merge the branch.
The guide quotes -> the draft body report instead of body text, and the drafter: reads it twice.
The bound stays at 18,000. characters for every host the server answers.`

// allCandidates lists every question DrawQuestion would draw from text.
func allCandidates(text string) []model.TextQuestion {
	var all []model.TextQuestion
	model.DrawQuestion(text, rand.New(rand.NewPCG(9, 9)), func(q model.TextQuestion) bool {
		all = append(all, q)
		return false
	})
	return all
}

func TestQuestionKindsNeedNoSentenceBoundary(t *testing.T) {
	for _, kind := range model.QuestionKinds {
		ask := model.TextQuestion{Kind: kind, Phrase: "a b c", Second: "d e f"}.Ask()
		if strings.Contains(ask, "sentence") || strings.Contains(ask, "line") {
			t.Errorf("no question kind should rest on where a sentence or line ends, got %q", ask)
		}
	}
}

func TestEveryQuestionReadsAsALiteralRunOfWords(t *testing.T) {
	sep := `[,.;:!?]? `
	end := `(?:[,.;:!?]|\s|$)`
	start := `(?:^|\s)`
	entryID := regexp.MustCompile(`(\d{8}-\d{6}-)?[a-z]-[a-z]{3}-[a-z0-9]+`)
	candidates := allCandidates(splitSample)
	if len(candidates) == 0 {
		t.Fatal("the sample should yield questions")
	}
	for _, q := range candidates {
		words := strings.Fields(q.Answer)
		quoted := make([]string, len(words))
		for i, w := range words {
			quoted[i] = regexp.QuoteMeta(w)
		}
		answer := strings.Join(quoted, sep)
		phrase, second := regexp.QuoteMeta(q.Phrase), regexp.QuoteMeta(q.Second)
		var window string
		switch q.Kind {
		case model.AskWordsAfter, model.AskNumberAfter:
			window = phrase + " " + answer + end
		case model.AskWordsBefore:
			window = start + answer + sep + phrase
		case model.AskBetween:
			window = phrase + " " + answer + sep + second
		default:
			t.Fatalf("unexpected kind %s", q.Kind)
		}
		if !regexp.MustCompile(window).MatchString(splitSample) {
			t.Errorf("the words asked for should stand next to the phrase as written, with nothing an agent counts between them, got %+v", q)
		}
		for _, part := range []string{q.Phrase, q.Second, q.Answer} {
			if entryID.MatchString(part) {
				t.Errorf("no question should ask about an entry ID, got %+v", q)
			}
			if strings.Contains(part, "model package") || strings.Contains(part, "every model") {
				t.Errorf("no question should ask about the words of a code span, got %+v", q)
			}
		}
		if len(q.Ask()) > 90 {
			t.Errorf("a question should stay short, got %d characters: %q", len(q.Ask()), q.Ask())
		}
	}
}

func TestNoAskedRunEndsOnAStopWord(t *testing.T) {
	text := "Then sdd writes them on the entry. The user wants to wrap up the session and leaves the room of the house to its owner."
	stop := map[string]bool{"the": true, "a": true, "an": true, "of": true, "on": true, "to": true, "up": true, "its": true, "and": true}
	candidates := allCandidates(text)
	if len(candidates) == 0 {
		t.Fatal("the sample should yield questions")
	}
	for _, q := range candidates {
		words := strings.Fields(q.Answer)
		if len(words) > 1 && stop[strings.ToLower(words[len(words)-1])] {
			t.Errorf("no question should ask for a run of words whose last word is a stop word, got %+v", q)
		}
	}
}

func TestQuestionsStayShortForLongWords(t *testing.T) {
	text := "Call configuration-resolution-precedence-override-mechanism before every-single-repository-synchronisation-handshake and the-whole-cross-project-dependency-closure-walk starts."
	for _, q := range allCandidates(text) {
		if len(q.Ask()) > 90 {
			t.Errorf("a question should stay short, got %q", q.Ask())
		}
	}
}

func TestRoundCredit(t *testing.T) {
	first := model.TextQuestion{Kind: model.AskWordsAfter, Phrase: "the draft must", Answer: "reach the"}
	second := model.TextQuestion{Kind: model.AskBetween, Phrase: "ask the user", Second: "confirm it now", Answer: "to"}
	pair := []model.TextQuestion{first, second}
	tests := []struct {
		name      string
		questions []model.TextQuestion
		given     string
		credited  []int
	}{
		{"both in order", pair, "reach the ; to", []int{0, 1}},
		{"no spaces", pair, "reach the;to", []int{0, 1}},
		{"swapped", pair, "to ; reach the", []int{0, 1}},
		{"first right, second wrong", pair, "reach the ; from", []int{0}},
		{"first wrong, second right", pair, "from ; to", []int{1}},
		{"right answer in the other slot beside a wrong one", pair, "from ; reach the", []int{0}},
		{"one answer for both", pair, "to ; to", []int{1}},
		{"both wrong", pair, "from ; into", nil},
		{"one of two", pair, "reach the", nil},
		{"one lost", pair, "reach the ; lost", nil},
		{"both lost", pair, "lost ; lost", nil},
		{"lost alone", pair, "lost", nil},
		{"three parts", pair, "reach the ; to ; to", nil},
		{"one question", pair[:1], "reach the", []int{0}},
		{"one question wrong", pair[:1], "from", nil},
		{"one question, two answers", pair[:1], "reach the ; to", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.RoundCredit(tc.questions, tc.given); !slices.Equal(got, tc.credited) {
				t.Errorf("RoundCredit should credit exactly the questions a right part answers, got %v", got)
			}
		})
	}
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
