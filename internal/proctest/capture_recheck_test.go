package proctest_test

import (
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	"github.com/networkteam/sdd/internal/relevance"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/local"
)

func strainOf(a, b, relation string) map[string]any {
	return map[string]any{"ids": []any{a, b}, "relation": relation}
}

// A reported strain becomes a recheck item with its relation bit in the
// ledger; while it stays unhealed a draft citing one of its entries is
// refused, a draft citing both heals it, and the healed item leaves the
// list (5.9, D26).
func TestCaptureRecheck_StrainHoldsDraftsCitingOneSideUntilHealed(t *testing.T) {
	events := local.NewMemoryEventStore()
	ledger := local.NewMemoryRelevanceStore()
	world, session := newCaptureWorld(t, "recheck-strain", proctest.WithGroundingEnforced(), proctest.WithRelevance(ledger), proctest.WithEvents(events))

	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Gather(t, instance, captureRef2ID)
	draft := captureDraft()
	draft["verdicts"] = []any{map[string]any{"ids": []any{captureRef2ID}, "code": model.VerdictCovered}}
	draft["strains"] = []any{strainOf(captureRef2ID, captureRefID, model.StrainContradicts)}
	serve = session.Report(t, instance, draft)
	proctest.RequireStep(t, serve, "playback")
	reporter := confirmCapture(t, world, session, instance)

	stored, err := events.Query(t.Context(), sdd.EventQuery{Channels: []string{model.RecheckChannel}, Types: []string{model.RecheckEventType}})
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || strings.Join(stored[0].Subject.Entries, ",") != captureRefID+","+captureRef2ID || !strings.Contains(string(stored[0].Payload), reporter.ID) {
		t.Fatalf("stored %+v: the strain should become one recheck item naming both entries and its reporter", stored)
	}
	segments, err := ledger.Segments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	folded, err := relevance.Fold(segments)
	if err != nil {
		t.Fatal(err)
	}
	pair, _ := folded.Reader(model.NewRelevancePair(captureRefID, captureRef2ID))
	if pair.Relations&model.RelationContradicts == 0 {
		t.Fatal("the strain's relation should be stored on the pair in the ledger")
	}

	serve = session.Start(t, "capture", nil)
	instance = serve.Instance
	session.Gather(t, instance)
	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "assemble")
	requireDiagnostics(t, serve, "cites "+captureRefID, "stays unhealed", stored[0].ID, "heal it by citing both")

	serve = session.Report(t, instance, map[string]any{"refs": []any{
		map[string]any{"id": captureRefID, "kind": "addresses", "desc": "resolves the clash"},
		map[string]any{"id": captureRef2ID, "kind": "addresses", "desc": "resolves the clash"},
	}})
	proctest.RequireStep(t, serve, "playback")
	confirmCapture(t, world, session, instance)

	serve = session.Start(t, "catch-up", nil)
	if strings.Contains(serve.Instructions, "## Recheck") {
		t.Fatal("a healed item should leave the recheck list")
	}
}

func TestCaptureRecheck_StrainsNameListedEntries(t *testing.T) {
	_, session := newCaptureWorld(t, "recheck-unlisted", proctest.WithGroundingEnforced(), proctest.WithEvents(local.NewMemoryEventStore()))
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Gather(t, instance)
	draft := captureDraft()
	draft["strains"] = []any{strainOf(captureRefID, groundingUnreadID, model.StrainOutdates)}
	serve = session.Report(t, instance, draft)
	proctest.RequireStep(t, serve, "assemble")
	requireDiagnostics(t, serve, "strains name entries the reading never listed", groundingUnreadID)
}

// Catch-up shows the recheck list only to the participants
// recheck.responsible names, to everyone while it names none (D5).
func TestCatchupRecheck_ShowsTheListToTheResponsibleParticipants(t *testing.T) {
	item := model.StrainItem(model.Strain{IDs: [2]string{"20260815-100000-d-tac-nxt", "20260816-120000-s-tac-gap"}, Relation: model.StrainOutdates}, "", time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC))
	payload, err := item.Payload()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name        string
		responsible []string
		shown       bool
	}{
		{"nobody named", nil, true},
		{"the participant named", []string{"Ada"}, true},
		{"someone else named", []string{"Grace"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			events := local.NewMemoryEventStore()
			err := events.Append(t.Context(), []sdd.Event{{ID: item.ID, Channel: model.RecheckChannel, Type: model.RecheckEventType, Time: item.At, Writer: "sdd",
				Subject: sdd.EventSubject{Entries: item.Entries}, Payload: payload}})
			if err != nil {
				t.Fatal(err)
			}
			world := proctest.NewWorld(t, proctest.WithEntries(catchupEntries()...), proctest.WithParticipant("Ada"), proctest.WithEvents(events),
				proctest.WithGrounding(sdd.GroundingPolicy{RecheckResponsible: tc.responsible}))
			serve := world.Open(t, "catchup-recheck").Start(t, "catch-up", nil)
			shown := strings.Contains(serve.Instructions, "## Recheck (1 open)") && strings.Contains(serve.Instructions, "Ship the next engine slice")
			if shown != tc.shown {
				t.Fatalf("recheck section shown = %v:\n%s", shown, serve.Instructions)
			}
		})
	}
}
