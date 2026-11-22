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

// A result at the bound prints whole in Codex's code mode, which spends the
// result's text at the rate measured there, 15,059 tokens for 29,131
// characters, against an output budget of 10,000 tokens.
func TestDefaultResultBoundFitsCodexOutputBudget(t *testing.T) {
	if tokens := int(model.DefaultResultBound) * 15059 / 29131; tokens > 10000 {
		t.Fatalf("a result at the bound should print within Codex's output budget, it costs %d tokens", tokens)
	}
}

// Only Claude Code, by its exact client name, gets the larger bound, lowered
// to two characters per token of MAX_MCP_OUTPUT_TOKENS, never below the
// default.
func TestResultBoundFor(t *testing.T) {
	for _, tc := range []struct {
		client string
		tokens int
		want   model.ResultBound
	}{
		{"claude-code", 0, model.ClaudeCodeResultBound},
		{"claude-code", 25000, model.ClaudeCodeResultBound},
		{"claude-code", 15000, 30000},
		{"claude-code", 5000, model.DefaultResultBound},
		{"claude-ai", 0, model.DefaultResultBound},
		{"Claude-Code", 0, model.DefaultResultBound},
		{"codex-mcp-client", 0, model.DefaultResultBound},
		{"", 0, model.DefaultResultBound},
	} {
		if got := model.ResultBoundFor(tc.client, tc.tokens); got != tc.want {
			t.Errorf("client %q with %d tokens should get its host's bound %d, got %d", tc.client, tc.tokens, tc.want, got)
		}
	}
}

// The larger bound stays below the 50,000 characters past which Claude Code
// saves a result to a file.
func TestClaudeCodeResultBoundStaysBelowItsFileThreshold(t *testing.T) {
	if model.ClaudeCodeResultBound >= 50000 {
		t.Fatal("Claude Code's bound should stay below its file threshold")
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

// A paragraph break early in the room does not end a chunk: a short heading
// followed by a long list of single lines, as gather's to-judge section is,
// fills the chunk with lines instead of leaving it holding the heading alone.
func TestSplitServedFillsAChunkPastAnEarlyParagraphBreak(t *testing.T) {
	text := "# to judge (60)\n\n" + strings.Repeat("- a candidate line to judge, one per entry\n", 60)
	chunks := model.SplitServed(text, 400)
	if got := model.ServedChars(chunks[0]); got <= 200 {
		t.Fatalf("the first chunk should carry lines past the heading, got %d characters: %q", got, chunks[0])
	}
	if !strings.HasSuffix(chunks[0], "\n") {
		t.Fatalf("the first chunk should end at a line break: %q", chunks[0])
	}
	if strings.Join(chunks, "") != text {
		t.Fatal("the chunks joined should be the text again")
	}
}
