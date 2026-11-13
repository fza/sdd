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
