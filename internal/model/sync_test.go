package model

import (
	"reflect"
	"regexp"
	"testing"
)

func TestCountMatchingSubjects(t *testing.T) {
	pattern := regexp.MustCompile(`^sdd:`)
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"one graph subject", "sdd: capture x\n", 1},
		{"mixed subjects", "sdd: capture x\nfeat: add y\nsdd: summarize z\n", 2},
		{"prefix not at start", "feat: sdd: y\n", 0},
		{"no trailing newline", "sdd: capture x", 1},
		{"blank line", "\nsdd: capture x\n", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CountMatchingSubjects(tc.in, pattern); got != tc.want {
				t.Errorf("only subjects starting with the prefix should count, got %d", got)
			}
		})
	}
}

func TestParseMergeTreeConflicts(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"clean merge — OID only", "abc1234567890abcdef\n", nil},
		{"clean merge — no trailing newline", "abc1234567890abcdef", nil},
		{"empty output", "", nil},
		{
			"single conflict",
			"abc1234567890abcdef\npath/to/file.go\n",
			[]string{"path/to/file.go"},
		},
		{
			"multiple conflicts",
			"treeoid\npath/a.go\npath/b.md\n",
			[]string{"path/a.go", "path/b.md"},
		},
		{
			"blank line terminates path list (trailing messages ignored)",
			"treeoid\npath/a.go\npath/b.md\n\nAuto-merging path/a.go\nCONFLICT (content): Merge conflict in path/a.go\n",
			[]string{"path/a.go", "path/b.md"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseMergeTreeConflicts(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}
