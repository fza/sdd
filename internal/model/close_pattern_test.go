package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestStandardClose(t *testing.T) {
	for _, tc := range []struct {
		name       string
		closerType model.EntryType
		closerKind model.Kind
		closedType model.EntryType
		closedKind model.Kind
		standard   bool
	}{
		{"plan addresses a gap", model.TypeDecision, model.KindPlan, model.TypeSignal, model.KindGap, true},
		{"done records a plan's completion", model.TypeSignal, model.KindDone, model.TypeDecision, model.KindPlan, true},
		{"done closes a gap in a short loop", model.TypeSignal, model.KindDone, model.TypeSignal, model.KindGap, true},
		{"fact dissolves a question", model.TypeSignal, model.KindFact, model.TypeSignal, model.KindQuestion, true},
		{"insight dissolves a question", model.TypeSignal, model.KindInsight, model.TypeSignal, model.KindQuestion, true},
		{"directive retires a fact", model.TypeDecision, model.KindDirective, model.TypeSignal, model.KindFact, true},
		{"directive retires an insight", model.TypeDecision, model.KindDirective, model.TypeSignal, model.KindInsight, true},
		{"directive retires a contract", model.TypeDecision, model.KindDirective, model.TypeDecision, model.KindContract, true},
		{"empty kinds read as a gap closing a directive", model.TypeSignal, "", model.TypeDecision, "", false},
		{"empty closer kind of a decision reads as directive", model.TypeDecision, "", model.TypeDecision, model.KindPlan, true},
		{"fact closes a fact", model.TypeSignal, model.KindFact, model.TypeSignal, model.KindFact, false},
		{"insight closes a fact", model.TypeSignal, model.KindInsight, model.TypeSignal, model.KindFact, false},
		{"gap closes a gap", model.TypeSignal, model.KindGap, model.TypeSignal, model.KindGap, false},
		{"done closes a fact", model.TypeSignal, model.KindDone, model.TypeSignal, model.KindFact, false},
		{"plan closes a plan", model.TypeDecision, model.KindPlan, model.TypeDecision, model.KindPlan, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := model.StandardClose(tc.closerType, tc.closerKind, tc.closedType, tc.closedKind)
			if got != tc.standard {
				t.Errorf("StandardClose() = %v, want %v", got, tc.standard)
			}
		})
	}
}
