package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

const mergeEntry = `---
type: signal
title: Old title
summary: 'Old summary: quoted'
future_key: kept
---

# body
`

func TestFrontmatterFields(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantOK  bool
		want    map[string]model.FrontmatterField
	}{
		{
			name:    "locates each key with its raw lines and decoded value",
			content: mergeEntry,
			wantOK:  true,
			want: map[string]model.FrontmatterField{
				"title":   {Present: true, Value: "Old title", Lines: "title: Old title\n"},
				"summary": {Present: true, Value: "Old summary: quoted", Lines: "summary: 'Old summary: quoted'\n"},
			},
		},
		{
			name:    "a multi-line value spans up to the next key",
			content: "---\nsummary: >-\n  first\n  second\ntype: signal\n---\n",
			wantOK:  true,
			want: map[string]model.FrontmatterField{
				"title":   {},
				"summary": {Present: true, Value: "first second", Lines: "summary: >-\n  first\n  second\n"},
			},
		},
		{
			name:    "the last key spans up to the closing delimiter",
			content: "---\ntype: signal\nsummary: last\n---\nbody\n",
			wantOK:  true,
			want: map[string]model.FrontmatterField{
				"title":   {},
				"summary": {Present: true, Value: "last", Lines: "summary: last\n"},
			},
		},
		{
			name:    "content without frontmatter is not handled",
			content: "# just markdown\n",
		},
		{
			name:    "an unclosed frontmatter is not handled",
			content: "---\ntitle: x\n",
		},
		{
			name:    "a non-scalar value under a handled key is not handled",
			content: "---\ntitle:\n  - a\n---\n",
		},
		{
			name:    "frontmatter that is no mapping is not handled",
			content: "---\n- a\n---\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields, ok := model.FrontmatterFields(tt.content, model.MergeResolvedKeys)
			if ok != tt.wantOK {
				t.Fatalf("handled = %v, want %v", ok, tt.wantOK)
			}
			for key, want := range tt.want {
				if fields[key] != want {
					t.Errorf("field %s = %+v, want %+v", key, fields[key], want)
				}
			}
		})
	}
}

func TestResolveMergedField(t *testing.T) {
	field := func(value string) model.FrontmatterField {
		return model.FrontmatterField{Present: true, Value: value, Lines: "summary: " + value + "\n"}
	}
	absent := model.FrontmatterField{}

	tests := []struct {
		name               string
		base, ours, theirs model.FrontmatterField
		want               model.FrontmatterField
	}{
		{name: "only theirs changed", base: field("a"), ours: field("a"), theirs: field("b"), want: field("b")},
		{name: "only ours changed", base: field("a"), ours: field("c"), theirs: field("a"), want: field("c")},
		{name: "both changed, ours sorts first", base: field("a"), ours: field("b"), theirs: field("c"), want: field("b")},
		{name: "both changed, theirs sorts first", base: field("a"), ours: field("c"), theirs: field("b"), want: field("b")},
		{name: "byte order, not locale", base: field("a"), ours: field("b"), theirs: field("B"), want: field("B")},
		{name: "both added", base: absent, ours: field("y"), theirs: field("x"), want: field("x")},
		{name: "only theirs added", base: absent, ours: absent, theirs: field("x"), want: field("x")},
		{name: "a removal against a change keeps the removal", base: field("a"), ours: absent, theirs: field("b"), want: absent},
		{
			name:   "a requoted unchanged value yields to the change",
			base:   field("a"),
			ours:   model.FrontmatterField{Present: true, Value: "a", Lines: "summary: 'a'\n"},
			theirs: field("b"),
			want:   field("b"),
		},
		{
			name:   "equal values written differently resolve by their lines",
			base:   field("a"),
			ours:   model.FrontmatterField{Present: true, Value: "b", Lines: "summary: 'b'\n"},
			theirs: field("b"),
			want:   model.FrontmatterField{Present: true, Value: "b", Lines: "summary: 'b'\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := model.ResolveMergedField(tt.base, tt.ours, tt.theirs)
			if got != tt.want {
				t.Errorf("resolved %+v, want %+v", got, tt.want)
			}
			swapped := model.ResolveMergedField(tt.base, tt.theirs, tt.ours)
			if swapped != got {
				t.Errorf("resolution should not depend on the merge direction: %+v against %+v", swapped, got)
			}
		})
	}
}

func TestReplaceFrontmatterField(t *testing.T) {
	tests := []struct {
		name    string
		content string
		key     string
		lines   string
		want    string
	}{
		{
			name:    "replaces the key's lines and keeps every other byte",
			content: mergeEntry,
			key:     "summary",
			lines:   "summary: New\n",
			want:    "---\ntype: signal\ntitle: Old title\nsummary: New\nfuture_key: kept\n---\n\n# body\n",
		},
		{
			name:    "empty lines remove the key",
			content: mergeEntry,
			key:     "title",
			lines:   "",
			want:    "---\ntype: signal\nsummary: 'Old summary: quoted'\nfuture_key: kept\n---\n\n# body\n",
		},
		{
			name:    "a missing key leaves the content alone",
			content: "---\ntype: signal\n---\n",
			key:     "title",
			lines:   "title: New\n",
			want:    "---\ntype: signal\n---\n",
		},
		{
			name:    "content without frontmatter stays as it is",
			content: "plain\n",
			key:     "title",
			lines:   "title: New\n",
			want:    "plain\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := model.ReplaceFrontmatterField(tt.content, tt.key, tt.lines)
			if got != tt.want {
				t.Errorf("content should carry the replacement and nothing else:\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}
