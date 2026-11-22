package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

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
