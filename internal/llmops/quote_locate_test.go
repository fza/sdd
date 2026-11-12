package llmops_test

import (
	"regexp"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/llmops"
)

var excerptPattern = regexp.MustCompile("“([^”]+)”|„([^“”]+)[“”]|\"([^\"]+)\"|«([^»]+)»|‚([^‘’]+)[‘’]|‘([^’]+)’|`([^`]+)`|(?:^|[^\\pL\\pN])'([^']+)'(?:$|[^\\pL\\pN])")

// citedExcerpts returns the excerpts an observation sets off in quotes or
// backticks. A straight single quote counts only next to a non-letter, so an
// apostrophe does not open an excerpt.
func citedExcerpts(observation string) []string {
	var out []string
	for _, m := range excerptPattern.FindAllStringSubmatch(observation, -1) {
		for _, g := range m[1:] {
			if g != "" {
				out = append(out, g)
				break
			}
		}
	}
	return out
}

func TestQuoteLocatesIn(t *testing.T) {
	const draft = "Die Fehlermeldung nutzt jetzt die zweite Variante aus dem Call, nicht mehr „Einladungslink“.\n" +
		"The release procedure gains this as a fixed section of its notes template. It gains this again."
	tests := []struct {
		name, quote, passage string
		want                 bool
	}{
		{"exact words", "gains this as a fixed section", "", true},
		{"ASCII quotes for typographic ones", `nicht mehr "Einladungslink"`, "", true},
		{"other case and whitespace", "THE RELEASE   procedure\ngains", "", true},
		{"shortened with an ellipsis", "release procedure … notes template", "", true},
		{"shortened with three dots", "release procedure ... notes template", "", true},
		{"parts out of order", "notes template … release procedure", "", false},
		{"words not in the draft", "gains this as a permanent section", "", false},
		{"empty quote", " „“ ", "", false},
		{"inside the passage", "zweite Variante aus dem Call", "Die Fehlermeldung nutzt jetzt die zweite Variante aus dem Call, nicht mehr „Einladungslink“.", true},
		{"outside the passage", "fixed section", "Die Fehlermeldung nutzt jetzt die zweite Variante aus dem Call, nicht mehr „Einladungslink“.", false},
		{"second occurrence inside the passage", "gains this", "It gains this again.", true},
		{"passage not in the draft", "gains this", "A sentence the draft lacks.", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := llmops.QuoteLocatesIn(draft, tt.quote, tt.passage); got != tt.want {
				t.Errorf("llmops.QuoteLocatesIn(%q, passage %q) = %v, want %v", tt.quote, tt.passage, got, tt.want)
			}
		})
	}
}

func TestCitedExcerpts(t *testing.T) {
	tests := []struct {
		name, observation string
		want              []string
	}{
		{
			"typographic, double quotes and backticks",
			"The ref `grounded-in` says “extends the contract” while the body says \"retires the immutability contract\"; „Partner-Code“ and «Link» stay.",
			[]string{"grounded-in", "extends the contract", "retires the immutability contract", "Partner-Code", "Link"},
		},
		{
			"single quotes beside apostrophes",
			"The draft's body states 'This directive retires the contract', but the desc doesn't: 'extends it'.",
			[]string{"This directive retires the contract", "extends it"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := citedExcerpts(tt.observation); !slices.Equal(got, tt.want) {
				t.Errorf("citedExcerpts = %q, want %q", got, tt.want)
			}
		})
	}
}
