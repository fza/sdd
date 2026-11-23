package model_test

import (
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// FitRef reads a ref against the applicability matrix: a target in the graph
// or a dependency gets its class, the chosen kind's cell and the kinds the
// class admits; an unresolvable target or a missing graph yields an
// unresolved fit, never a failure.
func TestFitRef(t *testing.T) {
	const depRepo model.RepoID = "example.test/b"
	decision := &model.Entry{ID: "20261101-100000-d-tac-dec", Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerTactical}
	done := &model.Entry{ID: "20261101-110000-s-tac-don", Type: model.TypeSignal, Kind: model.KindDone, Layer: model.LayerTactical}
	remote := &model.Entry{ID: "20261101-120000-s-tac-rem", Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical}
	graph := model.NewGraph([]*model.Entry{decision, done})
	dep := model.NewGraph([]*model.Entry{remote})
	model.NewMultiGraph(graph, []model.RepoID{depRepo}, func(model.RepoID) (*model.Graph, error) { return dep, nil })
	for _, tc := range []struct {
		name       string
		graph      *model.Graph
		ref        model.Ref
		class      model.RefTargetClass
		applicable bool
	}{
		{"live decision", graph, model.Ref{ID: decision.ID, Kind: model.RefKindRefines}, model.TargetLiveDecision, true},
		{"terminal done", graph, model.Ref{ID: done.ID, Kind: model.RefKindRefines}, model.TargetTerminalDone, false},
		{"dependency", graph, model.Ref{ID: string(depRepo) + ":" + remote.ID, Kind: model.RefKindAddresses}, model.TargetLiveSignal, true},
		{"unresolvable", graph, model.Ref{ID: "20261101-130000-s-tac-gon", Kind: model.RefKindRelated}, "", false},
		{"no graph", nil, model.Ref{ID: decision.ID, Kind: model.RefKindRefines}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fit := model.FitRef(tc.graph, tc.ref)
			if fit.Ref != tc.ref || fit.Resolved != (tc.class != "") || fit.Class != tc.class {
				t.Fatalf("the fit should carry the ref and its target's class %q, got %+v", tc.class, fit)
			}
			if tc.class == "" {
				return
			}
			cell, _ := model.RefKindApplicability(tc.ref.Kind, tc.class)
			if !fit.Capturable || fit.Cell != cell || fit.Cell.Applicable != tc.applicable {
				t.Fatalf("the fit should carry the chosen kind's cell, applicable %v, got %+v", tc.applicable, fit)
			}
			if !slices.Equal(fit.Admissible, model.AdmissibleRefKinds(tc.class)) {
				t.Fatalf("the fit should carry the kinds the class admits, got %v", fit.Admissible)
			}
		})
	}
}
