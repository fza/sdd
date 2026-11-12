package model_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// ServedChars counts what a text costs in an MCP result's text content, the
// structured copy's JSON: the text's JSON encoding, quotes left out.
func TestServedChars(t *testing.T) {
	for _, text := range []string{"", "plain words", "a \"quote\" and a \\ backslash", "lines\nand\ttabs\r", "<html> & more", "\x01 control", "ümlaut and 😀", "separators   "} {
		encoded, err := json.Marshal(text)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := model.ServedChars(text), len(encoded)-2; got != want {
			t.Errorf("%q should cost what its encoding takes, %d, got %d", text, want, got)
		}
		if prefix := model.ServedPrefix(text, model.ServedChars(text)-1); text != "" && prefix >= len(text) {
			t.Errorf("%q: a room short of the whole text should leave a prefix", text)
		}
	}
}

// SplitServed cuts text into chunks within the room, at the widest break each
// offers, that join to the text again.
func TestSplitServed(t *testing.T) {
	text := strings.Repeat("a line of a paragraph\n", 30) + "\n" + strings.Repeat("ü", 300)
	chunks := model.SplitServed(text, 400)
	if len(chunks) < 3 {
		t.Fatalf("the text should take several chunks, got %d", len(chunks))
	}
	for i, chunk := range chunks {
		if model.ServedChars(chunk) > 400 {
			t.Fatalf("chunk %d should stay within the room, got %d", i+1, model.ServedChars(chunk))
		}
		if i < len(chunks)-1 && strings.Contains(chunk, "line") && !strings.HasSuffix(chunk, "\n") {
			t.Fatalf("chunk %d should end at a line break", i+1)
		}
	}
	if strings.Join(chunks, "") != text {
		t.Fatal("the chunks joined should be the text again")
	}
}
