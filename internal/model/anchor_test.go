package model_test

import (
	"math/rand/v2"
	"strings"
	"testing"

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
