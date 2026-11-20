package model_test

import (
	"math/rand/v2"
	"regexp"
	"strings"
	"testing"
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
	text := "The capture `assemble` step reads 20260705-010751-d-tac-dbk and it is done.\n```\ncode words inside\n```\n**Grounding** matters."
	allowed := map[string]bool{"capture": true, "step": true, "reads": true, "done": true, "Grounding": true, "matters": true}
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

// A challenge asks for the content word right after a word that occurs once
// in the text: a function word after the prompt could be guessed from the
// pointer line alone, a repeated prompt names no one word.
func TestChallengeAnswersAreContentWords(t *testing.T) {
	text := "The parser reads the file and the cache of the index in the end, then writes the result to the store for the next run with the old marker.\nEach worker takes a lock and the queue drains into the archive."
	content := map[string]bool{}
	for _, w := range strings.Fields("parser reads file cache index end writes result store next run old marker worker takes lock queue drains archive") {
		content[w] = true
	}
	challenges := 0
	for seed := range uint64(200) {
		anchor, ok := model.PlaceAnchor(text, "ab12", seeded(seed))
		if !ok {
			t.Fatal("a text with content words should take an anchor")
		}
		if len(anchor.Challenges) > model.AnchorChallenges {
			t.Fatalf("an anchor should carry at most its challenge count, got %d", len(anchor.Challenges))
		}
		answers := map[string]bool{strings.ToLower(anchor.Word): true}
		for _, c := range anchor.Challenges {
			challenges++
			if !content[c.Answer] || !strings.Contains(text, c.Prompt+" "+c.Answer) {
				t.Fatalf("a challenge answer should be the content word right after its prompt, got %q after %q", c.Answer, c.Prompt)
			}
			if strings.Count(text, c.Prompt) != 1 {
				t.Fatalf("a challenge prompt should occur once in the text, got %q", c.Prompt)
			}
			if answers[strings.ToLower(c.Answer)] {
				t.Fatalf("each proof of an anchor should name another word, got %q twice", c.Answer)
			}
			answers[strings.ToLower(c.Answer)] = true
		}
	}
	if challenges == 0 {
		t.Fatal("a text with content-word pairs should yield challenges")
	}
}

// A word that also occurs inside another token, as "user" in "user's", is
// no prompt: the agent could not tell which spot it names.
func TestChallengePromptsOccurOnceAsAWord(t *testing.T) {
	text := "Settling them is the user's call; the user actually decides each review."
	for seed := range uint64(100) {
		anchor, ok := model.PlaceAnchor(text, "ab12", seeded(seed))
		if !ok {
			t.Fatal("a text with content words should take an anchor")
		}
		for _, c := range anchor.Challenges {
			if c.Prompt == "user" {
				t.Fatalf("a word occurring inside another token should not be a prompt, got %+v", c)
			}
		}
	}
}

// A word the piece holds once but the result around it holds again, as an
// entry body's word in its heading, is no prompt.
func TestChallengePromptsOccurOnceInTheField(t *testing.T) {
	body := "Later the reviewer approves drafts quickly and the approver merges branches."
	field := model.CountWords("## The reviewer's choice\n\n" + body)
	sawPrompt := false
	for seed := range uint64(100) {
		anchor, ok := model.PlaceAnchorIn(body, field, "ab12", seeded(seed))
		if !ok {
			t.Fatal("a text with content words should take an anchor")
		}
		for _, c := range anchor.Challenges {
			sawPrompt = true
			if c.Prompt == "reviewer" {
				t.Fatalf("a word the field holds twice should not be a prompt, got %+v", c)
			}
		}
	}
	if !sawPrompt {
		t.Fatal("precondition: the body should yield challenges")
	}
}

func TestNextProofQuotesThePrompt(t *testing.T) {
	if got := model.NextProof("ab12", "graph"); got != `next proof ab12:<the word right after "graph">` {
		t.Errorf("the next proof should name the tag and quote the prompt, got %q", got)
	}
	if got := model.NextProof("ab12", ""); got != "" {
		t.Errorf("without a prompt there should be no next proof, got %q", got)
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

// A schema whose tag sits in a description of one plain pair still takes its
// follow-up prompts, from the other descriptions, each prompt once in the
// whole schema, its answer on the prompt's own line.
func TestSchemaAnchorTakesPromptsFromEveryDescription(t *testing.T) {
	descriptions := map[string]string{
		"draft":  "markdown body",
		"layer":  "layer naming purpose; markdown words",
		"topics": "topic labels grouping entries",
		"refs":   "related entries cited inline",
	}
	properties := map[string]any{}
	for name, desc := range descriptions {
		properties[name] = map[string]any{"type": "string", "description": desc}
	}
	schema := map[string]any{"type": "object", "properties": properties}
	whole := strings.Join([]string{descriptions["draft"], descriptions["layer"], descriptions["topics"], descriptions["refs"]}, "\n")
	for seed := range uint64(40) {
		anchor, ok := model.PlaceSchemaAnchor(schema, "zz99", seeded(seed))
		if !ok {
			t.Fatal("a schema with described properties should take an anchor")
		}
		if len(anchor.Challenges) != model.AnchorChallenges {
			t.Fatalf("the anchor should take every follow-up prompt the schema offers, got %+v", anchor.Challenges)
		}
		for _, c := range anchor.Challenges {
			if c.Prompt == "markdown" || !strings.Contains(whole, c.Prompt+" "+c.Answer) {
				t.Fatalf("each prompt should occur once in the schema, its answer right after it, got %+v", c)
			}
		}
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
