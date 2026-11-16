package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestSplitStagedBody(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantHeader bool
		wantBody   string
	}{
		{name: "a header block is cut off", content: "---\nlayer: tactical\n---\nBody line.\n", wantHeader: true, wantBody: "Body line.\n"},
		{name: "a file without a block is all body", content: "Body line.\n---\nmore\n", wantBody: "Body line.\n---\nmore\n"},
		{name: "an unclosed block is all body", content: "---\nlayer: tactical\nBody line.\n", wantBody: "---\nlayer: tactical\nBody line.\n"},
		{name: "CRLF delimiters count", content: "---\r\nlayer: tactical\r\n---\r\nBody\r\n", wantHeader: true, wantBody: "Body\r\n"},
		{name: "a byte order mark before the first delimiter counts", content: "\uFEFF---\nlayer: tactical\n---\nBody\n", wantHeader: true, wantBody: "Body\n"},
		{name: "a byte order mark without a header stays out of the body", content: "\uFEFFBody\n", wantBody: "Body\n"},
		{name: "a delimiter with trailing spaces is no delimiter", content: "--- \nlayer: tactical\n---\nBody\n", wantBody: "--- \nlayer: tactical\n---\nBody\n"},
		{name: "a block that is no valid YAML is body", content: "---\n: : :\n---\nBody\n", wantBody: "---\n: : :\n---\nBody\n"},
		{name: "a block that is no mapping is body", content: "---\n- a\n---\nBody\n", wantBody: "---\n- a\n---\nBody\n"},
		{name: "a mapping without a compared key is body", content: "---\ntitle: Draft\n---\nBody\n", wantBody: "---\ntitle: Draft\n---\nBody\n"},
		{name: "a markdown rule opening the body keeps its first section", content: "---\nThe first section.\n\n---\nThe second section.\n", wantBody: "---\nThe first section.\n\n---\nThe second section.\n"},
		{name: "a compared key beside other keys makes a header", content: "---\ntitle: Draft\nlayer: tac\n---\nBody\n", wantHeader: true, wantBody: "Body\n"},
		{name: "a block scalar line opening with a hash makes a header", content: "---\nlayer: tac\nintent: |\n  # x\n---\nBody\n", wantHeader: true, wantBody: "Body\n"},
		{name: "a hash line in a block without a compared key is body", content: "---\n# Context\nThe gist: short\n---\nRest\n", wantBody: "---\n# Context\nThe gist: short\n---\nRest\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header, body, ambiguous := model.SplitStagedBody(tt.content)
			if (header != nil) != tt.wantHeader {
				t.Errorf("header presence should match the block, got %v", header)
			}
			if body != tt.wantBody {
				t.Errorf("body should be the text after the block:\n%q\nwant\n%q", body, tt.wantBody)
			}
			if ambiguous {
				t.Error("a block reading one way only should not be ambiguous")
			}
		})
	}
}

// A block holding a compared key and a hash line at the margin reads as a
// commented header and as a markdown heading alike.
func TestSplitStagedBodyAmbiguous(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "a heading before a blank line and a key", content: "---\n# Context\n\nlayer: the tactical layer is where this lives\n---\nRest\n"},
		{name: "a heading before a line shaped like a key", content: "---\n# Context\nlayer: the tactical layer is where this lives\n---\nRest\n"},
		{name: "a heading before prose and a key", content: "---\n# Context\nThe gist: short\nlayer: tac\n---\nRest\n"},
		{name: "a comment line before the keys", content: "---\n# header\nentryKind: gap\nlayer: tac\n---\nBody\n"},
		{name: "a comment line closing the keys", content: "---\nlayer: tac\n# trailing\n---\nBody\n"},
		{name: "a comment line before a blank line", content: "---\n# c\n\nlayer: tac\n---\nBody\n"},
		{name: "a comment line before an indented item", content: "---\nrefs:\n# c\n  - a\n---\nBody\n"},
		{name: "a comment line before an item at the margin", content: "---\nrefs:\n# c\n- a\n---\nBody\n"},
		{name: "a comment line before a quoted key", content: "---\n# c\n\"layer\": tac\n---\nBody\n"},
		{name: "CRLF lines", content: "---\r\n# c\r\nlayer: tac\r\n---\r\nBody\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header, body, ambiguous := model.SplitStagedBody(tt.content)
			if !ambiguous {
				t.Fatal("the block should be ambiguous")
			}
			if header != nil || body != tt.content {
				t.Errorf("an ambiguous block should leave the whole file body, got header %v, body %q", header, body)
			}
		})
	}
}

func TestStagedHeaderMismatch(t *testing.T) {
	report := map[string]any{
		"entryKind":  "directive",
		"layer":      "tactical",
		"confidence": "high",
		"refs": []any{
			map[string]any{"id": "20990101-100000-d-tac-aaa", "kind": "refines", "desc": "why"},
			map[string]any{"id": "20990101-110000-d-tac-bbb", "kind": "builds-on"},
		},
		"topics": []any{"agent/ux", "portability/mcp"},
		"closes": []any{"20990101-120000-s-tac-ccc"},
	}
	tests := []struct {
		name    string
		header  string
		wantKey string
	}{
		{name: "equal values pass", header: "entryKind: directive\nlayer: tac\nconfidence: high\ntopics: [portability/mcp, agent/ux]\ncloses: [20990101-120000-s-tac-ccc]\n"},
		{name: "a differing scalar is named", header: "confidence: medium\n", wantKey: "confidence"},
		{name: "a differing layer is named", header: "layer: conceptual\n", wantKey: "layer"},
		{
			name:   "refs in report form compare regardless of order",
			header: "refs:\n  - {id: 20990101-110000-d-tac-bbb, kind: builds-on}\n  - {id: 20990101-100000-d-tac-aaa, kind: refines, desc: why}\n",
		},
		{name: "a differing ref is named", header: "refs:\n  - {id: 20990101-100000-d-tac-aaa, kind: related, desc: why}\n  - {id: 20990101-110000-d-tac-bbb, kind: builds-on}\n", wantKey: "refs"},
		{name: "refs in another shape are not compared", header: "refs:\n  - refines 20990101-100000-d-tac-aaa: why\n"},
		{name: "a ref with an extra key is another shape", header: "refs:\n  - {id: 20990101-100000-d-tac-aaa, kind: related, note: x}\n"},
		{name: "topics as one string are another shape", header: "topics: agent/ux, other\n"},
		{name: "a differing topic list is named", header: "topics: [agent/ux]\n", wantKey: "topics"},
		{name: "closes holding no entry IDs are another shape", header: "closes: [gar]\n"},
		{name: "a key the report does not state is not compared", header: "intent: settled\n"},
		{name: "keys outside the report fields are ignored", header: "kind: gap\ntitle: anything\nconfidence: high\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header, _, _ := model.SplitStagedBody("---\n" + tt.header + "---\nBody\n")
			if header == nil {
				t.Fatal("the test header should parse")
			}
			key, mismatch := model.StagedHeaderMismatch(header, report)
			if mismatch != (tt.wantKey != "") || key != tt.wantKey {
				t.Errorf("mismatch should name the differing key only, got %q (%v)", key, mismatch)
			}
		})
	}
}
