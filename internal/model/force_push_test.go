package model_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestReaddsAfterForcePush(t *testing.T) {
	const kept, gone = "20261107-110000-d-tac-dcs", "20261107-100000-d-tac-gon"
	graph := model.NewGraph([]*model.Entry{{ID: kept, Type: model.TypeDecision, Layer: model.LayerTactical}})
	cases := []struct {
		name  string
		entry model.Entry
		want  bool
	}{
		{name: "refines a kept entry", entry: model.Entry{Refs: []model.Ref{{ID: kept, Kind: model.RefKindRefines}}}, want: true},
		{name: "supersedes a kept entry", entry: model.Entry{Supersedes: []string{kept}}, want: true},
		{name: "plain new entry", entry: model.Entry{}},
		{name: "grounded in a kept entry only", entry: model.Entry{Refs: []model.Ref{{ID: kept, Kind: model.RefKindGroundedIn}}}},
		{name: "refines an entry the rewrite dropped", entry: model.Entry{Refs: []model.Ref{{ID: gone, Kind: model.RefKindRefines}}}},
		{name: "refines a kept entry and names a dropped one", entry: model.Entry{
			Refs: []model.Ref{{ID: kept, Kind: model.RefKindRefines}, {ID: gone, Kind: model.RefKindGroundedIn}},
		}},
		{name: "supersedes a kept entry and closes a dropped one", entry: model.Entry{Supersedes: []string{kept}, Closes: []string{gone}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.ReaddsAfterForcePush(&tc.entry, graph); got != tc.want {
				t.Errorf("the entry should return on its own only when it supersedes or refines and all it names resolves, got %v", got)
			}
		})
	}
}

func TestRederiveFields(t *testing.T) {
	const before = "---\ntype: signal\ntitle: Old title\nsummary: Old summary.\n---\n\nBody.\n"
	cases := []struct {
		name, after, current, want string
	}{
		{
			name:    "a changed title lands on the rewritten entry",
			after:   "---\ntype: signal\ntitle: New title\nsummary: Old summary.\n---\n\nBody.\n",
			current: "---\ntype: signal\ntitle: Old title\nsummary: Rewritten summary.\n---\n\nRewritten body.\n",
			want:    "---\ntype: signal\ntitle: New title\nsummary: Rewritten summary.\n---\n\nRewritten body.\n",
		},
		{
			name:    "a title the rewritten entry lacks is added",
			after:   "---\ntype: signal\ntitle: New title\nsummary: Old summary.\n---\n\nBody.\n",
			current: "---\ntype: signal\nsummary: Old summary.\n---\n\nBody.\n",
			want:    "---\ntype: signal\nsummary: Old summary.\ntitle: New title\n---\n\nBody.\n",
		},
		{
			name:    "fields sdd left alone keep the rewritten values",
			after:   before + "More body.\n",
			current: "---\ntype: signal\ntitle: Other title\nsummary: Other summary.\n---\n\nBody.\n",
			want:    "---\ntype: signal\ntitle: Other title\nsummary: Other summary.\n---\n\nBody.\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.RederiveFields(before, tc.after, tc.current); got != tc.want {
				t.Errorf("only sdd's change of a derived field should carry over, got:\n%s", got)
			}
		})
	}
}

func TestSetAsideNotice(t *testing.T) {
	block := model.SetAside{
		Old: "1111111111111111", New: "2222222222222222", Dir: "/data/sdd/repos/x/.git/sdd-set-aside/files",
		Entries: []string{".sdd/graph/2026/11/07-121000-s-tac-pln.md"},
	}.Block()
	owned := block.Claim("s_1", block.Since)
	notice := owned.NoticeFor("s_1")
	for _, part := range []string{"111111111111", "222222222222", block.Detail, block.Paths[0], "Re-capture"} {
		if !strings.Contains(notice, part) {
			t.Errorf("the owner's notice should name the rewrite, where the entries lie, and what to do:\n%s", notice)
		}
	}
	if block.Pauses() || block.ClearedByPush() {
		t.Error("set-aside entries should neither pause sync nor clear with a push")
	}
}
