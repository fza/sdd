package model_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestFirstSentenceEndsAtTheFirstBoundary(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want string
	}{
		{name: "period", text: "It commits to one thing. It relates to another.", want: "It commits to one thing."},
		{name: "line break first", text: "A heading line\nand a body sentence.", want: "A heading line"},
		{name: "single sentence", text: "Nothing follows it", want: "Nothing follows it"},
		{name: "empty", text: "   ", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.FirstSentence(tc.text); got != tc.want {
				t.Errorf("first sentence should end at the first boundary, got %q", got)
			}
		})
	}
}

// A first sentence written as a paragraph would push every other column off a
// brief line, so the cut happens at a word boundary and says that it happened.
func TestShortTitleCutsAtAWordBoundary(t *testing.T) {
	long := "The project repository carries only the work, while signals and decisions live in a checkout beside it"

	got := model.ShortTitle(long, 40)
	if len([]rune(got)) > 41 {
		t.Errorf("cut should respect the bound, got %d runes: %q", len([]rune(got)), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a cut title should say it was cut, got %q", got)
	}
	if strings.Contains(got, "car…") || strings.Contains(got, " …") {
		t.Errorf("cut should fall between words, got %q", got)
	}
}

func TestShortTitleLeavesShortTextAlone(t *testing.T) {
	for _, tc := range []struct {
		name     string
		text     string
		maxRunes int
	}{
		{name: "under the bound", text: "Graph commits follow the graph's repository.", maxRunes: 110},
		{name: "unbounded", text: strings.Repeat("word ", 60), maxRunes: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.ShortTitle(tc.text, tc.maxRunes); got != strings.TrimSpace(tc.text) {
				t.Errorf("text within the bound should pass through unchanged, got %q", got)
			}
		})
	}
}

// Multi-byte text must not be cut mid-rune.
func TestShortTitleKeepsRunesWhole(t *testing.T) {
	got := model.ShortTitle("Entscheidungen über Zuständigkeiten müssen nachvollziehbar bleiben", 20)
	if !strings.ContainsRune(got, 'ü') && !strings.HasSuffix(got, "…") {
		t.Errorf("unexpected cut: %q", got)
	}
	for _, r := range got {
		if r == '�' {
			t.Fatalf("cut split a rune: %q", got)
		}
	}
}
