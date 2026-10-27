package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func chainEntry(t *testing.T, id string, kind model.Kind, canonical string, supersedes ...string) *model.Entry {
	t.Helper()
	parts, err := model.ParseID(id)
	if err != nil {
		t.Fatalf("bad test ID %q: %v", id, err)
	}
	return &model.Entry{
		ID:         id,
		Type:       model.TypeFromAbbrev[parts.TypeCode],
		Layer:      model.LayerProcess,
		Kind:       kind,
		Canonical:  canonical,
		Supersedes: supersedes,
		Content:    canonical,
		Time:       parts.Time,
	}
}

// A duplicated actor captured as two chains is merged by one entry
// superseding both heads (20261026-144315-s-tac-5y1).
func TestActorChains_MergeJoinsSupersededChains(t *testing.T) {
	first := chainEntry(t, "20260825-140417-s-prc-aaa", model.KindActor, "Marta")
	second := chainEntry(t, "20260826-223224-s-prc-bbb", model.KindActor, "Marta")
	role := &model.Entry{
		ID:      "20260826-230913-d-prc-rol",
		Type:    model.TypeDecision,
		Layer:   model.LayerProcess,
		Kind:    model.KindRole,
		Actor:   "Marta",
		Refs:    []model.Ref{{ID: second.ID, Kind: model.RefKindGroundedIn}},
		Content: "Marta selects the repertoire",
	}
	merger := chainEntry(t, "20261026-142851-s-prc-ccc", model.KindActor, "Marta", first.ID, second.ID)

	g := model.NewGraph([]*model.Entry{first, second, role, merger})

	chains := g.ChainsForCanonical("Marta")
	if len(chains) != 1 {
		t.Fatalf("chains holding the canonical = %d, want 1", len(chains))
	}
	if chains[0].Head == nil || chains[0].Head.ID != merger.ID {
		t.Errorf("head = %v, want %s", chains[0].Head, merger.ID)
	}
	if len(chains[0].Entries) != 3 {
		t.Errorf("chain entries = %d, want 3", len(chains[0].Entries))
	}
	for _, e := range []*model.Entry{first, second, merger} {
		for _, w := range e.Warnings {
			if w.Field == "canonical" {
				t.Errorf("%s carries canonical warning %q", e.ID, w.Message)
			}
		}
	}
	if got := g.DerivedStatus(role).Kind; got != model.StatusActive {
		t.Errorf("role status = %s, want %s", got, model.StatusActive)
	}
}

func TestProcedureChains_MergeJoinsSupersededChains(t *testing.T) {
	first := chainEntry(t, "20260825-140417-d-prc-aaa", model.KindProcedure, "release")
	second := chainEntry(t, "20260826-223224-d-prc-bbb", model.KindProcedure, "release")
	merger := chainEntry(t, "20261026-142851-d-prc-ccc", model.KindProcedure, "release", first.ID, second.ID)

	g := model.NewGraph([]*model.Entry{first, second, merger})

	var holding []*model.ProcedureChain
	for _, c := range g.ProcedureChains() {
		if c.HasCanonical("release") {
			holding = append(holding, c)
		}
	}
	if len(holding) != 1 {
		t.Fatalf("chains holding the canonical = %d, want 1", len(holding))
	}
	if holding[0].Head == nil || holding[0].Head.ID != merger.ID {
		t.Errorf("head = %v, want %s", holding[0].Head, merger.ID)
	}
}
