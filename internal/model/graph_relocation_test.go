package model_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestEntryShaped(t *testing.T) {
	cases := map[string]bool{
		"2026/11/08-120000-s-tac-abc.md":       true,
		"2026/11/08-120000-d-cpt-a1b.md":       true,
		"2026/11/08-120000-s-tac-abc/notes.md": false,
		"wip/2026/11/08-120000-s-tac-abc.md":   false,
		"2026/11/08-120000.md":                 false,
		"README.md":                            false,
	}
	for path, want := range cases {
		if got := model.EntryShaped(path); got != want {
			t.Errorf("EntryShaped(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestGraphRelocationUnsettled(t *testing.T) {
	relocation := model.GraphRelocation{From: "/repo/.sdd/graph", To: "/repo/graph", Entries: 3}
	all := []string{"2026/11/08-120000-s-tac-aaa.md", "2026/11/08-120001-s-tac-bbb.md", "2026/11/08-120002-s-tac-ccc.md"}
	cases := []struct {
		name     string
		to, from model.GraphFolder
		want     string
	}{
		{name: "missing", to: model.GraphFolder{}, from: model.GraphFolder{Exists: true, Entries: all}, want: "does not exist"},
		{name: "empty", to: model.GraphFolder{Exists: true}, from: model.GraphFolder{Exists: true, Entries: all}, want: "holds no entries"},
		{name: "entries left behind", to: model.GraphFolder{Exists: true, Entries: all[:2]}, from: model.GraphFolder{Exists: true, Entries: all[2:]}, want: "1 entries in /repo/.sdd/graph, the folder served before, are missing from /repo/graph"},
		{name: "fewer entries", to: model.GraphFolder{Exists: true, Entries: all[:2]}, from: model.GraphFolder{}, want: "holds 2 entries, /repo/.sdd/graph held 3"},
		{name: "settled", to: model.GraphFolder{Exists: true, Entries: all}, from: model.GraphFolder{Exists: true}},
		{name: "a checkout keeping the same entries", to: model.GraphFolder{Exists: true, Entries: all}, from: model.GraphFolder{Exists: true, Entries: all}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := relocation.Unsettled(tc.to, tc.from)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("a complete move should be settled: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want an error naming %q, got %v", tc.want, err)
			}
		})
	}
}
