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
		{name: "a block that is no valid YAML still splits off", content: "---\n: : :\n---\nBody\n", wantBody: "Body\n"},
		{name: "a block that is no mapping still splits off", content: "---\n- a\n---\nBody\n", wantBody: "Body\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header, body := model.SplitStagedBody(tt.content)
			if (header != nil) != tt.wantHeader {
				t.Errorf("header presence should match the block, got %v", header)
			}
			if body != tt.wantBody {
				t.Errorf("body should be the text after the block:\n%q\nwant\n%q", body, tt.wantBody)
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
			map[string]any{"id": "20261030-120506-d-tac-l2b", "kind": "refines", "desc": "why"},
			map[string]any{"id": "20260826-120330-d-tac-8f8", "kind": "builds-on"},
		},
		"topics": []any{"agent/ux", "portability/mcp"},
		"closes": []any{"20261030-125047-s-tac-gar"},
	}
	tests := []struct {
		name    string
		header  string
		wantKey string
	}{
		{name: "equal values pass", header: "entryKind: directive\nlayer: tac\nconfidence: high\ntopics: [portability/mcp, agent/ux]\ncloses: [20261030-125047-s-tac-gar]\n"},
		{name: "a differing scalar is named", header: "confidence: medium\n", wantKey: "confidence"},
		{name: "a differing layer is named", header: "layer: conceptual\n", wantKey: "layer"},
		{
			name:   "refs in report form compare regardless of order",
			header: "refs:\n  - {id: 20260826-120330-d-tac-8f8, kind: builds-on}\n  - {id: 20261030-120506-d-tac-l2b, kind: refines, desc: why}\n",
		},
		{name: "a differing ref is named", header: "refs:\n  - {id: 20261030-120506-d-tac-l2b, kind: related, desc: why}\n  - {id: 20260826-120330-d-tac-8f8, kind: builds-on}\n", wantKey: "refs"},
		{name: "refs in another shape are not compared", header: "refs:\n  - refines 20261030-120506-d-tac-l2b: why\n"},
		{name: "a ref with an extra key is another shape", header: "refs:\n  - {id: 20261030-120506-d-tac-l2b, kind: related, note: x}\n"},
		{name: "topics as one string are another shape", header: "topics: agent/ux, other\n"},
		{name: "a differing topic list is named", header: "topics: [agent/ux]\n", wantKey: "topics"},
		{name: "closes holding no entry IDs are another shape", header: "closes: [gar]\n"},
		{name: "a key the report does not state is not compared", header: "intent: settled\n"},
		{name: "keys outside the report fields are ignored", header: "kind: gap\ntitle: anything\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header, _ := model.SplitStagedBody("---\n" + tt.header + "---\nBody\n")
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
