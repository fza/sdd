package model_test

import (
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

const (
	strainA = "20261101-120000-d-tac-aaa"
	strainB = "20261102-120000-d-tac-bbb"
	other   = "20261103-120000-s-tac-ccc"
)

func recheckGraph(extra ...*model.Entry) *model.Graph {
	entries := []*model.Entry{
		{ID: strainA, Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerTactical, Time: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)},
		{ID: strainB, Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerTactical, Time: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)},
		{ID: other, Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical, Time: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)},
	}
	return model.NewGraph(append(entries, extra...))
}

func TestStrainItem_IsOneItemWhicheverWayItIsReported(t *testing.T) {
	at := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	forward := model.StrainItem(model.Strain{IDs: [2]string{strainA, strainB}, Relation: model.StrainContradicts}, other, at)
	backward := model.StrainItem(model.Strain{IDs: [2]string{strainB, strainA}, Relation: model.StrainContradicts}, other, at.Add(time.Hour))
	if forward.ID != backward.ID || forward.Entries[0] != strainA {
		t.Fatal("the same strain reported twice should be one item")
	}
	if model.StrainItem(model.Strain{IDs: [2]string{strainA, strainB}, Relation: model.StrainDuplicates}, other, at).ID == forward.ID {
		t.Fatal("another relation between the same entries should be its own item")
	}
	payload, err := forward.Payload()
	if err != nil {
		t.Fatal(err)
	}
	back, err := model.RecheckItemFromEvent(forward.ID, forward.At, forward.Entries, payload)
	if err != nil {
		t.Fatal(err)
	}
	if back.Kind != forward.Kind || back.Relation != forward.Relation || back.ReportedBy != other {
		t.Fatalf("item %+v should read back from its event", back)
	}
	if _, err := model.RecheckItemFromEvent("x", at, []string{strainA}, payload); err == nil {
		t.Fatal("a strain event naming one entry should be refused")
	}
}

func TestUnhealedRecheck(t *testing.T) {
	at := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	strain := model.StrainItem(model.Strain{IDs: [2]string{strainA, strainB}, Relation: model.StrainContradicts}, other, at)
	later := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		extra  []*model.Entry
		healed bool
	}{
		{"no later entry", nil, false},
		{"a later entry citing one", []*model.Entry{{ID: "20261105-000000-d-tac-ddd", Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerTactical, Time: later, Refs: []model.Ref{{ID: strainA}}}}, false},
		{"a later entry citing both", []*model.Entry{{ID: "20261105-000000-d-tac-ddd", Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerTactical, Time: later, Refs: []model.Ref{{ID: strainA}, {ID: strainB}}}}, true},
		{"an earlier entry citing both", []*model.Entry{{ID: "20261103-000000-d-tac-ddd", Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerTactical, Time: time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC), Refs: []model.Ref{{ID: strainA}, {ID: strainB}}}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := recheckGraph(tc.extra...)
			open := model.UnhealedRecheck(g, []model.RecheckItem{strain})
			healed := model.HealedRecheck(g, []model.RecheckItem{strain})
			if (len(open) == 0) != tc.healed || (len(healed) == 1) != tc.healed {
				t.Fatalf("open %v, healed %v", open, healed)
			}
		})
	}
	reporter := model.StrainItem(model.Strain{IDs: [2]string{strainA, strainB}, Relation: model.StrainContradicts}, "20261105-000000-d-tac-ddd", at)
	g := recheckGraph(&model.Entry{ID: "20261105-000000-d-tac-ddd", Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerTactical, Time: later, Refs: []model.Ref{{ID: strainA}, {ID: strainB}}})
	if len(model.UnhealedRecheck(g, []model.RecheckItem{reporter})) != 1 {
		t.Fatal("the capture that reported a strain should not heal it")
	}
	gone := recheckGraph(&model.Entry{ID: "20261105-000000-d-tac-ddd", Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerTactical, Time: later, Supersedes: []string{strainB}})
	edit := model.HandEditItem(strainB, "abc", later.Add(time.Hour))
	if len(model.UnhealedRecheck(gone, []model.RecheckItem{edit})) != 0 {
		t.Fatal("an item whose entry is no longer live should leave the list, even when no later entry cites it")
	}
}

// D26: a draft citing one entry of an unhealed strain is refused; a draft
// citing neither, or both, passes.
func TestStrainRefusals(t *testing.T) {
	strain := model.StrainItem(model.Strain{IDs: [2]string{strainA, strainB}, Relation: model.StrainContradicts}, other, time.Time{})
	edit := model.HandEditItem(strainA, "abc", time.Time{})
	open := []model.RecheckItem{strain, edit}
	if got := model.StrainRefusals(open, []string{strainA, other}); len(got) != 1 || got[0].ID != strain.ID {
		t.Fatalf("refusals %v: a draft citing A should be held by the A-B strain alone", got)
	}
	if got := model.StrainRefusals(open, []string{other}); len(got) != 0 {
		t.Fatal("a draft citing neither entry should pass")
	}
	if got := model.StrainRefusals(open, []string{strainA, strainB}); len(got) != 0 {
		t.Fatal("a draft citing both entries heals the strain and should pass")
	}
}
