package model_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

const entryWithUnknownFields = "---\n" +
	"type: decision\n" +
	"layer: tactical\n" +
	"kind: directive\n" +
	"title: Keep unknown fields\n" +
	"reason: |-\n" +
	"    Another tool reads these.\n" +
	"    Two lines.\n" +
	"decided-by: agent\n" +
	"approves:\n" +
	"    - 20260101-100000-s-tac-aaa\n" +
	"grounding:\n" +
	"    read: [20260101-100000-s-tac-aaa]\n" +
	"    rounds: 2\n" +
	"summary: Old summary.\n" +
	"summary_hash: deadbeef\n" +
	"---\n\nBody.\n"

// A rewrite keeps every frontmatter key the binary does not read, value and
// order as parsed, after the keys it writes itself.
func TestFormatFrontmatterKeepsUnknownFields(t *testing.T) {
	e, err := model.ParseEntry("20260101-120000-d-tac-abc.md", entryWithUnknownFields)
	if err != nil {
		t.Fatalf("ParseEntry: %v", err)
	}
	var keys []string
	for _, f := range e.UnknownFields {
		keys = append(keys, f.Key)
	}
	if got, want := strings.Join(keys, ","), "title,reason,decided-by,approves,grounding"; got != want {
		t.Fatalf("unknown keys = %s, want %s", got, want)
	}

	e.Summary = "New summary."
	got := model.FormatFrontmatter(e)
	want := "---\n" +
		"type: decision\n" +
		"layer: tactical\n" +
		"kind: directive\n" +
		"summary: New summary.\n" +
		"title: Keep unknown fields\n" +
		"reason: |-\n" +
		"    Another tool reads these.\n" +
		"    Two lines.\n" +
		"decided-by: agent\n" +
		"approves:\n" +
		"    - 20260101-100000-s-tac-aaa\n" +
		"grounding:\n" +
		"    read: [20260101-100000-s-tac-aaa]\n" +
		"    rounds: 2\n" +
		"---\n"
	if got != want {
		t.Errorf("FormatFrontmatter:\n%s\nwant:\n%s", got, want)
	}

	again, err := model.ParseEntry("20260101-120000-d-tac-abc.md", got+"\nBody.\n")
	if err != nil {
		t.Fatalf("ParseEntry of the rewrite: %v", err)
	}
	if out := model.FormatFrontmatter(again); out != got {
		t.Errorf("second rewrite changed the frontmatter:\n%s\nwant:\n%s", out, got)
	}
}

// An unknown value holding an alias is written back resolved, so the rewrite
// never points at an anchor it does not write.
func TestFormatFrontmatterResolvesAliasesInUnknownFields(t *testing.T) {
	content := "---\n" +
		"type: signal\n" +
		"layer: tactical\n" +
		"kind: gap\n" +
		"participants: &who [Jane]\n" +
		"seen-by: *who\n" +
		"---\n\nBody.\n"
	e, err := model.ParseEntry("20260101-120000-s-tac-abc.md", content)
	if err != nil {
		t.Fatalf("ParseEntry: %v", err)
	}
	out := model.FormatFrontmatter(e)
	if !strings.Contains(out, "seen-by:\n    - Jane\n") {
		t.Errorf("alias not resolved:\n%s", out)
	}
	if _, err := model.ParseEntry("20260101-120000-s-tac-abc.md", out+"\nBody.\n"); err != nil {
		t.Errorf("rewrite does not parse: %v\n%s", err, out)
	}
}
