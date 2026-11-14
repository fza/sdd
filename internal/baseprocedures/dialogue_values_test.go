package baseprocedures_test

import (
	"regexp"
	"slices"
	"strconv"
	"testing"

	"github.com/networkteam/sdd/internal/baseprocedures"
	"github.com/networkteam/sdd/internal/model"
)

var (
	modeFirst   = regexp.MustCompile(`\b(?:eq|ne)\s+\.dialogueMode((?:\s+(?:"[^"]*"|` + "`[^`]*`" + `))+)`)
	literalLast = regexp.MustCompile(`\b(?:eq|ne)\s+("[^"]*"|` + "`[^`]*`" + `)\s+\.dialogueMode\b`)
	literal     = regexp.MustCompile(`"[^"]*"|` + "`[^`]*`")
)

// A comparison against a value the model does not know never matches, so
// its branch would silently never serve.
func TestBaseProceduresCompareTheDialogueModeOnlyToKnownValues(t *testing.T) {
	entries, err := baseprocedures.Entries()
	if err != nil {
		t.Fatal(err)
	}
	compared := 0
	for _, entry := range entries {
		var literals []string
		for _, m := range modeFirst.FindAllStringSubmatch(entry.Content, -1) {
			literals = append(literals, literal.FindAllString(m[1], -1)...)
		}
		for _, m := range literalLast.FindAllStringSubmatch(entry.Content, -1) {
			literals = append(literals, m[1])
		}
		for _, quoted := range literals {
			compared++
			value, err := strconv.Unquote(quoted)
			if err != nil {
				t.Fatalf("%s.md: unquoting %s: %v", entry.ID, quoted, err)
			}
			if !slices.Contains(model.DialogueModes(), model.DialogueMode(value)) {
				t.Errorf("%s.md compares .dialogueMode to %q, which is not one of %v", entry.ID, value, model.DialogueModes())
			}
		}
	}
	if compared == 0 {
		t.Error("the base procedures should compare .dialogueMode somewhere; the pattern no longer finds them")
	}
}
