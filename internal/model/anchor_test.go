package model_test

import (
	"math/rand/v2"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/networkteam/sdd/internal/model"
)

func seeded(seed uint64) *rand.Rand { return rand.New(rand.NewPCG(seed, seed)) }

func TestNewAnchorTagIsFourLowercaseBase36Characters(t *testing.T) {
	for seed := range uint64(50) {
		tag := model.NewAnchorTag(seeded(seed))
		if _, err := model.ParseProof(tag + ":word"); err != nil {
			t.Fatalf("a drawn tag should read back as a proof's tag: %v", err)
		}
		if strings.ToLower(tag) != tag || len(tag) != 4 {
			t.Fatalf("a tag should be four lowercase characters, got %q", tag)
		}
	}
}

func TestPlaceAnchorStandsBeforeAContentWord(t *testing.T) {
	text := "The capture `assemble` step reads 20260705-010751-d-tac-dbk and it is done.\n```\ncode words inside\n```\n**Grounding** matters.\nEvery later capture checks its draft against the graph first."
	allowed := map[string]bool{"capture": true, "step": true, "reads": true, "done": true, "Grounding": true, "matters": true,
		"Every": true, "later": true, "checks": true, "draft": true, "against": true, "graph": true, "first": true}
	seen := map[string]bool{}
	for seed := range uint64(200) {
		anchor, ok := model.PlaceAnchor(text, "ab12", seeded(seed))
		if !ok {
			t.Fatal("a text with content words should take an anchor")
		}
		if !allowed[anchor.Word] {
			t.Fatalf("the tag should stand before a content word outside code, got %q", anchor.Word)
		}
		rendered := anchor.Render(text)
		word, ok := model.WordAfterAnchor(rendered, "ab12")
		if !ok || word != anchor.Word {
			t.Fatalf("the word read after the tag should be the anchored word, got %q in %q", word, rendered)
		}
		if strings.ReplaceAll(rendered, "⟦ab12⟧ ", "") != text {
			t.Fatal("rendering should only insert the tag")
		}
		seen[anchor.Word] = true
	}
	if len(seen) < 3 {
		t.Errorf("placement should vary with the random source, saw %v", seen)
	}
}

// A challenge's answer is a content word: a function word after the prompt
// could be guessed from the pointer line alone.
func TestChallengeAnswersAreContentWords(t *testing.T) {
	text := "The parser reads the file and the cache of the index in the end, then writes the result to the store for the next run with the old marker.\nEach worker takes a lock and the queue drains into the archive."
	content := map[string]bool{}
	for _, w := range strings.Fields("parser reads file cache index end writes result store next run old marker Each worker takes lock queue drains archive") {
		content[w] = true
	}
	challenges := 0
	for seed := range uint64(200) {
		anchor, ok := model.PlaceAnchor(text, "ab12", seeded(seed))
		if !ok {
			t.Fatal("a text with content words should take an anchor")
		}
		for _, c := range anchor.Challenges {
			challenges++
			if !content[c.Answer] {
				t.Fatalf("a challenge answer should be a content word, got %q after %q", c.Answer, c.Prompt)
			}
			if !strings.Contains(text, c.Prompt+" "+c.Answer) {
				t.Fatalf("a challenge answer should be the word right after its prompt, got %q after %q", c.Answer, c.Prompt)
			}
		}
	}
	if challenges == 0 {
		t.Fatal("a text with content-word pairs should yield challenges")
	}
}

// A prompt occurs once as a run of letters anywhere in the piece, code and
// compound words included, so the agent's lookup finds the one spot it names.
func TestChallengePromptsOccurOnceAsAWord(t *testing.T) {
	letterRuns := func(text string) []string {
		return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) })
	}
	for _, text := range []string{
		"The user's choice settles the review. Later the user approves every staged change.",
		"Set the `user` field before the parser reads it. Later the user approves every staged change.",
		"A user-facing page lists the review. Later the user approves every staged change.",
	} {
		for seed := range uint64(200) {
			anchor, ok := model.PlaceAnchor(text, "ab12", seeded(seed))
			if !ok {
				t.Fatal("a text with content words should take an anchor")
			}
			for _, c := range anchor.Challenges {
				if n := slices.Index(letterRuns(text), strings.ToLower(c.Prompt)); n < 0 || slices.Contains(letterRuns(text)[n+1:], strings.ToLower(c.Prompt)) {
					t.Fatalf("prompt %q should occur once as a word in %q", c.Prompt, text)
				}
			}
		}
	}
}

// A piece joined with others into one result asks for words that occur once
// in the whole result, so a word of a piece beside it never answers.
func TestChallengePromptsOccurOnceInTheField(t *testing.T) {
	text := "The focus lane renders every staged change before the parser reads it."
	field := "Another lane ranks the parser output.\n\n" + text
	for seed := range uint64(200) {
		anchor, ok := model.PlaceAnchorIn(text, field, "ab12", seeded(seed))
		if !ok {
			t.Fatal("a text with content words should take an anchor")
		}
		for _, c := range anchor.Challenges {
			if c.Prompt == "lane" || c.Prompt == "parser" {
				t.Fatalf("a prompt occurring in a piece beside it should not be drawn, got %+v", c)
			}
		}
	}
}

func TestPlaceAnchorKeepsALaterProofInAShortPiece(t *testing.T) {
	text := "Local participant: Tester\nSearch: text\nBase branch: main (configured default)"
	for seed := range uint64(200) {
		anchor, ok := model.PlaceAnchor(text, "ab12", seeded(seed))
		if !ok {
			t.Fatal("a text with content words should take an anchor")
		}
		if len(anchor.Challenges) == 0 {
			t.Fatalf("a short piece with a content-word pair should keep a later proof, the tag before %q left none", anchor.Word)
		}
	}
}

func TestPlaceAnchorRefusesTextWithoutContentWords(t *testing.T) {
	for _, text := range []string{"", "a to of", "`code only`", "20260705-010751-d-tac-dbk", "key:"} {
		if _, ok := model.PlaceAnchor(text, "ab12", seeded(1)); ok {
			t.Errorf("no content word should take no anchor: %q", text)
		}
	}
}

func TestSchemaAnchorSitsInADescription(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"draft": map[string]any{"type": "string", "description": "the entry body as markdown"},
		},
	}
	anchor, ok := model.PlaceSchemaAnchor(schema, "zz99", seeded(3))
	if !ok {
		t.Fatal("a schema with a described property should take an anchor")
	}
	tagged := anchor.RenderSchema(schema)
	desc := tagged["properties"].(map[string]any)["draft"].(map[string]any)["description"].(string)
	if word, _ := model.WordAfterAnchor(desc, "zz99"); word != anchor.Word {
		t.Errorf("the tag should sit before the anchored word in the description, got %q", desc)
	}
	if schema["properties"].(map[string]any)["draft"].(map[string]any)["description"] != "the entry body as markdown" {
		t.Error("rendering should leave the source schema untouched")
	}
}

func TestProofParsingAndMatching(t *testing.T) {
	cases := []struct {
		in      string
		want    model.Proof
		invalid bool
	}{
		{in: "ab12:capture", want: model.Proof{Tag: "ab12", Word: "capture"}},
		{in: " ⟦AB12⟧ : Capture ", want: model.Proof{Tag: "ab12", Word: "Capture"}},
		{in: "ab12", invalid: true},
		{in: "ab1:word", invalid: true},
		{in: "ab12:", invalid: true},
	}
	for _, tc := range cases {
		got, err := model.ParseProof(tc.in)
		if tc.invalid {
			if err == nil {
				t.Errorf("%q should be refused as a proof", tc.in)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%q should parse to its tag and word, got %v %v", tc.in, got, err)
		}
	}
	if !model.ProofWordMatches("Grounding", "grounding,") {
		t.Error("a proof word should match case-insensitively with punctuation trimmed")
	}
	if model.ProofWordMatches("Grounding", "ground") || model.ProofWordMatches("Grounding", "") {
		t.Error("a different or empty word should not match")
	}
}

func TestReadAnchorsReadsEveryTagWithItsWord(t *testing.T) {
	text := `{"a":"x ⟦ab12⟧ first\nmore ⟦cd34⟧ second.","b":"⟦bad⟧ skip"}`
	got := model.ReadAnchors(text)
	want := []model.Proof{{Tag: "ab12", Word: "first"}, {Tag: "cd34", Word: "second"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("every well-formed tag should read with its word, got %v", got)
	}
}

// A text that already carries a tag, as a lane carrying a tagged entry, takes
// a second tag only where it leaves the first one's word in place.
func TestPlaceAnchorKeepsAnotherTagsWord(t *testing.T) {
	text := "Draft ⟦ab12⟧ grounding across ⟦cd34⟧ entries"
	for seed := range uint64(200) {
		anchor, ok := model.PlaceAnchor(text, "zz99", seeded(seed))
		if !ok {
			t.Fatal("a text with free content words should take an anchor")
		}
		rendered := anchor.Render(text)
		for _, proof := range []model.Proof{{Tag: "ab12", Word: "grounding"}, {Tag: "cd34", Word: "entries"}} {
			word, _ := model.WordAfterAnchor(rendered, proof.Tag)
			if word != proof.Word {
				t.Fatalf("a new tag should not stand between another tag and its word, got %q", rendered)
			}
		}
	}
}

// plainTokenPattern is a plain word once surrounding punctuation is trimmed:
// letters, a hyphen only inside.
var plainTokenPattern = regexp.MustCompile(`^\p{L}+(-\p{L}+)*$`)

func isPlainToken(token string) bool {
	word := strings.Trim(token, `"'()[]{}*_.,;:!?`)
	if !plainTokenPattern.MatchString(word) {
		return false
	}
	letters := utf8.RuneCountInString(strings.ReplaceAll(word, "-", ""))
	return letters >= 3 && letters <= 12
}

// tagNeighbours reads the token right before tag's mark on its line and the
// token right after it.
func tagNeighbours(t *testing.T, rendered, tag string) (before, after string) {
	t.Helper()
	mark := model.AnchorMark(tag)
	i := strings.Index(rendered, mark)
	if i < 0 {
		t.Fatalf("no mark %s in %q", mark, rendered)
	}
	line := rendered[strings.LastIndex(rendered[:i], "\n")+1 : i]
	if fields := strings.Fields(line); len(fields) > 0 {
		before = fields[len(fields)-1]
	}
	if fields := strings.Fields(rendered[i+len(mark):]); len(fields) > 0 {
		after = fields[0]
	}
	return before, after
}

func assertPlainPlaces(t *testing.T, text string) {
	t.Helper()
	for seed := range uint64(200) {
		anchor, ok := model.PlaceAnchor(text, "ab12", seeded(seed))
		if !ok {
			t.Fatal("a text with content words should take an anchor")
		}
		before, after := tagNeighbours(t, anchor.Render(text), "ab12")
		if !isPlainToken(before) || !isPlainToken(after) {
			t.Fatalf("both words beside the tag should be plain 3-12 letter words, got %q ⟦ab12⟧ %q", before, after)
		}
		for _, c := range anchor.Challenges {
			if !isPlainToken(c.Prompt) || !isPlainToken(c.Answer) {
				t.Fatalf("a challenge should name and ask for plain 3-12 letter words, got %+v", c)
			}
		}
	}
}

func TestPlaceAnchorKeepsAURLAwayFromTheTag(t *testing.T) {
	assertPlainPlaces(t, "Read the guide at https://docs.example.com/guides/proof-markers/placement/before-plain-words?ref=served today, then confirm the marker placement works.")
}

func TestPlaceAnchorNeverChoosesAnEntryIDOrAPath(t *testing.T) {
	assertPlainPlaces(t, "Read 20260705-010751-d-tac-dbk first, then open internal/model/anchor.go quickly and edit ~/.config settings before merging the branch.")
}

func TestPlaceAnchorStandsBetweenPlainWords(t *testing.T) {
	assertPlainPlaces(t, "The internationalization layer calls PlaceAnchorIn twice per request, so caching helps 42 workers.\nEach worker reads `sdd view` output and writes summaries of the result.")
}

func TestPlaceAnchorFallsBackWithoutAPlainPlace(t *testing.T) {
	for _, text := range []string{"Internationalization", "Internationalization considerations", "## Grounding"} {
		anchor, ok := model.PlaceAnchor(text, "ab12", seeded(1))
		if !ok {
			t.Errorf("a text without a plain place should still take an anchor: %q", text)
			continue
		}
		if word, _ := model.WordAfterAnchor(anchor.Render(text), "ab12"); word == "" || word != anchor.Word {
			t.Errorf("the fallback tag should stand before a content word, got %q in %q", word, text)
		}
	}
}

func TestStripAnchorTags(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"a placed tag", "serve each entry ⟦q7m2⟧ once per session", "serve each entry once per session"},
		{"at the start", "⟦ab12⟧ Draft one entry", "Draft one entry"},
		{"no tag", "Draft one entry", "Draft one entry"},
		{"brackets that are no tag", "a ⟦notes⟧ stays", "a ⟦notes⟧ stays"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.StripAnchorTags(tc.in); got != tc.want {
				t.Errorf("StripAnchorTags(%q) = %q, should take out only placed tags", tc.in, got)
			}
		})
	}
}

// A schema's proofs ask for words once in the whole schema the agent reads,
// property names and the other descriptions included.
func TestSchemaChallengePromptsOccurOnceInTheSchema(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"reviewer": map[string]any{"type": "string", "description": "Later the reviewer approves drafts before every write lands."},
			"note":     map[string]any{"type": "string", "description": "A note on drafts the parser reads."},
		},
	}
	for seed := range uint64(200) {
		anchor, ok := model.PlaceSchemaAnchor(schema, "ab12", seeded(seed))
		if !ok {
			t.Fatal("a schema with descriptions should take an anchor")
		}
		for _, c := range anchor.Challenges {
			if c.Prompt == "reviewer" || c.Prompt == "drafts" {
				t.Fatalf("a prompt occurring elsewhere in the schema should not be drawn, got %+v", c)
			}
		}
	}
}
