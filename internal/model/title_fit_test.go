package model_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/networkteam/sdd/internal/model"
)

func TestFitTitle(t *testing.T) {
	// 60 characters; the cut point is the space after them.
	head := "Deep verifier exchange across child sessions and delivery ab"
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{name: "short title is only capitalized", title: "daemon project removal", want: "Daemon project removal"},
		{name: "78 characters stay whole", title: head + " words words words", want: head + " words words words"},
		{name: "over 80 cut after 60 when two words go", title: head + " words words words words", want: head + "…"},
		{name: "over 80 kept when the cut drops one word", title: head + " wordswordswordswordswordswords", want: head + " wordswordswordswordswordswords"},
		{name: "backticks and punctuation do not count", title: head + " `aa`? `bb`? `cc`? `dd`?", want: head + " `aa`? `bb`? `cc`? `dd`?"},
		{name: "over 80 without a space after 60 stays whole", title: head + "xxxxxxxxxxxxxxxxxxxxxxxx", want: head + "xxxxxxxxxxxxxxxxxxxxxxxx"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.FitTitle(tc.title); got != tc.want {
				t.Errorf("FitTitle(%d chars) = %q, want %q", utf8.RuneCountInString(tc.title), got, tc.want)
			}
		})
	}
}

func TestFitTitleCutsWithinBounds(t *testing.T) {
	title := strings.Repeat("word ", 40)
	got := model.FitTitle(title)
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("a 200-character title should be cut with an ellipsis, got %q", got)
	}
	if n := utf8.RuneCountInString(got); n <= model.TitleCutAt || n > model.TitleCutAbove {
		t.Errorf("the cut title should hold more than %d and at most %d characters, got %d: %q", model.TitleCutAt, model.TitleCutAbove, n, got)
	}
}
