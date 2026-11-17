package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestUserWordsOwed(t *testing.T) {
	cases := []struct {
		kind           model.Kind
		reason, intent bool
	}{
		{model.KindDirective, true, true},
		{model.KindPlan, true, false},
		{model.KindRole, true, false},
		{model.KindGap, false, false},
		{model.KindDone, false, false},
		{"", false, false},
	}
	for _, c := range cases {
		t.Run(string(c.kind), func(t *testing.T) {
			reason, intent := model.UserWordsOwed(c.kind)
			if reason != c.reason || intent != c.intent {
				t.Errorf("UserWordsOwed(%q) = (%v, %v), want (%v, %v)", c.kind, reason, intent, c.reason, c.intent)
			}
		})
	}
}

func TestReasonRepeatsIntent(t *testing.T) {
	cases := []struct {
		name, reason, intent string
		repeats              bool
	}{
		{"reason holds the intent", "We decided the parser module should reject empty input with a clear error instead of returning an empty result.", "the parser module should reject empty input with a clear error", true},
		{"intent holds the reason", "reject empty input", "The parser should reject empty input.", true},
		{"case, spacing and closing punctuation set aside", "Reject  Empty input!", "reject empty input", true},
		{"a reason of its own", "empty input hid upstream bugs twice", "the parser module should reject empty input with a clear error", false},
		{"the intent with a because clause", "always run the linter because CI blocks otherwise", "always run the linter", false},
		{"the intent with a since clause", "Always run the linter, since CI blocks otherwise.", "always run the linter", false},
		{"no intent", "empty input hid upstream bugs twice", "", false},
		{"no reason", "", "reject empty input", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := model.ReasonRepeatsIntent(c.reason, c.intent); got != c.repeats {
				t.Errorf("ReasonRepeatsIntent(%q, %q) = %v", c.reason, c.intent, got)
			}
		})
	}
}

func TestQuotedFrom(t *testing.T) {
	message := `Looks right, but say "an explicit error" instead of "a clear error".`
	for _, tc := range []struct {
		name  string
		words string
		want  bool
	}{
		{"part of the message", "say an explicit error", true},
		{"other case and quotes", "Say “An explicit error”", true},
		{"closing punctuation", "instead of a clear error.", true},
		{"words the user did not say", "reject empty input with an explicit error", false},
		{"a word inside another word", "rig", false},
		{"a letter of a word", "e", false},
		{"empty", "  ", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.QuotedFrom(tc.words, message); got != tc.want {
				t.Errorf("QuotedFrom(%q) should be %v", tc.words, tc.want)
			}
		})
	}
}

func TestQuotedFromMatchesWholeWords(t *testing.T) {
	if model.QuotedFrom("no", "I know the reason") {
		t.Error(`"no" inside "know" should not count as quoted`)
	}
	if !model.QuotedFrom("know the reason", "I know the reason.") {
		t.Error("whole words of the message should count as quoted")
	}
}

// An approval says yes to the draft and gives no why of the person's.
func TestOnlyApproval(t *testing.T) {
	for _, tc := range []struct {
		words string
		want  bool
	}{
		{"ok", true},
		{"OK!", true},
		{"Yes, go ahead.", true},
		{"lgtm", true},
		{"approved, write it", true},
		{"passt", true},
		{"ok, a red build blocks the team", false},
		{"because the team waits", false},
		{"", false},
	} {
		if got := model.OnlyApproval(tc.words); got != tc.want {
			t.Errorf("OnlyApproval(%q) should be %v", tc.words, tc.want)
		}
	}
}
