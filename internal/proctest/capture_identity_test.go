// Capture behavior for the identity kinds: actor and role flow through the
// shared capture procedure without the refs+topics the gate demands of
// ordinary kinds, playback renders the identity fields, and a role's bound
// actor must resolve. Ported from internal/engine.
package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
)

const identityActorID = "20260601-100000-s-prc-act"

// newIdentityWorld is newCaptureWorld plus one actor (canonical
// "Christopher"), so role captures resolve and participantsCanonical leaves
// grace mode with the resolved participant still valid.
func newIdentityWorld(t *testing.T, connID string) (*proctest.World, *proctest.Session) {
	t.Helper()
	entries := append(captureFixtureEntries(), &model.Entry{
		ID: identityActorID, Type: model.TypeSignal, Kind: model.KindActor, Layer: model.LayerProcess, Canonical: "Christopher",
		Summary: "Christopher, the known actor role captures bind to.",
		Content: "Christopher, the known actor role captures bind to.",
	})
	world := proctest.NewWorld(t, proctest.WithEntries(entries...))
	session := world.Open(t, connID)
	session.LogRead(t, "show", []string{captureRefID}, nil)
	return world, session
}

// actorDraft drafts a NEW actor: the real write gate blocks reusing an
// existing chain's canonical (actor-canonical-reused), so unlike the engine
// fixture the drafted identity must be distinct from the fixture actor.
func actorDraft() map[string]any {
	return map[string]any{
		"body":        "Maria is CEO of networkteam with a full-stack background — the identity she brings from outside this project.",
		"entryKind":   "actor",
		"layer":       "process",
		"canonical":   "Maria",
		"aliases":     []any{"Mia", "MW"},
		"confidence":  "high",
		"widenReport": "checked the graph for an existing Maria actor before drafting",
	}
}

func roleDraft() map[string]any {
	return map[string]any{
		"body":        "Christopher is the principal developer here and holds the strategic and conceptual calls.",
		"entryKind":   "role",
		"layer":       "process",
		"roleActor":   "Christopher",
		"confidence":  "high",
		"widenReport": "confirmed the Christopher actor exists to bind this role",
	}
}

func TestCapture_ActorGatePassesWithoutRefsOrTopics(t *testing.T) {
	world, session := newIdentityWorld(t, "identity-actor")
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance

	serve = session.Report(t, instance, actorDraft())
	proctest.RequireStep(t, serve, "playback")
	if !strings.Contains(serve.Instructions, "canonical: Maria") {
		t.Errorf("playback should render the canonical, got %q", serve.Instructions)
	}
	if !strings.Contains(serve.Instructions, "Mia") || !strings.Contains(serve.Instructions, "MW") {
		t.Errorf("playback should render aliases, got %q", serve.Instructions)
	}

	serve = session.Answer(t, instance, "playback", "confirm", nil, "that's me")
	proctest.RequireStep(t, serve, "verifySummary")
	serve = session.Answer(t, instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	proctest.RequireStatus(t, serve, "completed")
	entryID, _ := serve.Produced["entryId"].(string)
	if entryID == "" {
		t.Fatalf("actor capture produced no entryId: %+v", serve.Produced)
	}
	entry := proctest.LoadEntry(t, world.GraphDir, entryID)
	if entry.Canonical != "Maria" {
		t.Fatalf("persisted canonical = %q", entry.Canonical)
	}
}

func TestCapture_ActorMissingCanonicalStalls(t *testing.T) {
	_, session := newIdentityWorld(t, "identity-no-canonical")
	serve := session.Start(t, "capture", nil)
	draft := actorDraft()
	delete(draft, "canonical")
	serve = session.Report(t, serve.Instance, draft)
	proctest.RequireStep(t, serve, "assemble")
	if !hasDiagnostic(serve, "does not satisfy its kind's structural rules") {
		t.Fatalf("diagnostics = %v, want the draftValidates message (canonical is the actor kind's structural rule)", serve.Diagnostics)
	}
}

func TestCapture_ActorMalformedAliasStalls(t *testing.T) {
	_, session := newIdentityWorld(t, "identity-bad-alias")
	serve := session.Start(t, "capture", nil)
	draft := actorDraft()
	// An alias repeating the canonical is malformed.
	draft["aliases"] = []any{"Maria"}
	serve = session.Report(t, serve.Instance, draft)
	proctest.RequireStep(t, serve, "assemble")
	if !hasDiagnostic(serve, "does not satisfy its kind's structural rules") {
		t.Fatalf("diagnostics = %v, want the draftValidates message (malformed alias)", serve.Diagnostics)
	}
}

func TestCapture_RoleGateResolvesBoundActor(t *testing.T) {
	_, session := newIdentityWorld(t, "identity-role")
	serve := session.Start(t, "capture", nil)
	serve = session.Report(t, serve.Instance, roleDraft())
	proctest.RequireStep(t, serve, "playback")
	if !strings.Contains(serve.Instructions, "roleActor: Christopher") {
		t.Errorf("playback draft block should carry the bound actor, got %q", serve.Instructions)
	}
}

func TestCapture_RoleUnresolvableActorBlocks(t *testing.T) {
	_, session := newIdentityWorld(t, "identity-ghost-actor")
	serve := session.Start(t, "capture", nil)
	draft := roleDraft()
	draft["roleActor"] = "Ghost"
	serve = session.Report(t, serve.Instance, draft)
	proctest.RequireStep(t, serve, "assemble")
	if !hasDiagnostic(serve, "roleActor does not name an actor known to the graph") {
		t.Fatalf("diagnostics = %v, want the roleActorResolves message", serve.Diagnostics)
	}
}

func TestCapture_RecognitionModeSoftensPlayback(t *testing.T) {
	_, session := newIdentityWorld(t, "identity-recognition")
	serve := session.Start(t, "capture", nil)
	draft := actorDraft()
	draft["recognitionMode"] = true
	serve = session.Report(t, serve.Instance, draft)
	proctest.RequireStep(t, serve, "playback")
	if !strings.Contains(serve.Instructions, "recognition") {
		t.Errorf("recognition-mode playback should soften to recognition framing, got %q", serve.Instructions)
	}
	// The invariant holds: the body is still shown verbatim.
	if !strings.Contains(serve.Instructions, "CEO of networkteam") {
		t.Errorf("recognition-mode playback must still render the body verbatim, got %q", serve.Instructions)
	}
}

func TestCapture_OrdinaryKindStrayCanonicalBlocks(t *testing.T) {
	_, session := newIdentityWorld(t, "identity-stray-canonical")
	serve := session.Start(t, "capture", nil)
	// A canonical on a gap is a stray per-kind field — the boundary's
	// projection reports it instead of a kind branch silently ignoring it.
	serve = session.Report(t, serve.Instance, map[string]any{
		"body":        "A tactical gap carrying a stray canonical.",
		"entryKind":   "gap",
		"layer":       "tactical",
		"canonical":   "Christopher",
		"confidence":  "medium",
		"widenReport": "widened",
	})
	proctest.RequireStep(t, serve, "assemble")
	if !hasDiagnostic(serve, "does not satisfy its kind's structural rules") {
		t.Fatalf("diagnostics = %v, want the draftValidates message (stray canonical)", serve.Diagnostics)
	}
}

// TestCapture_RootEntryPassesWithoutRefsOrTopics pins the boundary-delegated
// gate: refs and topics are not per-kind requirements the type-system
// contract backs, so a genuinely root entry is not forced to invent a
// reference and a topic-less draft passes to the guide.
func TestCapture_RootEntryPassesWithoutRefsOrTopics(t *testing.T) {
	_, session := newIdentityWorld(t, "identity-root")
	serve := session.Start(t, "capture", nil)
	serve = session.Report(t, serve.Instance, map[string]any{
		"body":        "A genuinely root observation with nothing upstream to point at.",
		"entryKind":   "gap",
		"layer":       "tactical",
		"confidence":  "medium",
		"widenReport": "searched for prior art from several angles; nothing bears on this",
	})
	if serve.Step == "assemble" {
		t.Fatalf("a root entry must pass assemble without refs or topics, diagnostics=%v", serve.Diagnostics)
	}
}

func TestCapture_ActorLandsWithItsActorKind(t *testing.T) {
	world, session := newIdentityWorld(t, "identity-actor-kind")
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance

	draft := actorDraft()
	draft["actorKind"] = "human"
	serve = session.Report(t, instance, draft)
	proctest.RequireStep(t, serve, "playback")
	if !strings.Contains(serve.Instructions, "actorKind: human") {
		t.Errorf("playback should render the actor kind beside the identity fields, got %q", serve.Instructions)
	}

	serve = session.Answer(t, instance, "playback", "confirm", nil, "that's me")
	proctest.RequireStep(t, serve, "verifySummary")
	serve = session.Answer(t, instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	proctest.RequireStatus(t, serve, "completed")
	entryID, _ := serve.Produced["entryId"].(string)
	entry := proctest.LoadEntry(t, world.GraphDir, entryID)
	if entry.ActorKind != model.ActorKindHuman {
		t.Errorf("the reported actor kind should persist on the written actor, got %q", entry.ActorKind)
	}
}

func TestCapture_ActorKindOutsideTheSortsIsRefused(t *testing.T) {
	_, session := newIdentityWorld(t, "identity-actor-kind-invalid")
	serve := session.Start(t, "capture", nil)
	draft := actorDraft()
	draft["actorKind"] = "robot"
	_, err := session.ReportErr(t, serve.Instance, draft)
	if err == nil || !strings.Contains(err.Error(), "actorKind") {
		t.Fatalf("an actor kind outside the sorts should be refused by name, got %v", err)
	}
}

func TestCapture_ActorKindOnANonActorStalls(t *testing.T) {
	_, session := newIdentityWorld(t, "identity-actor-kind-stray")
	serve := session.Start(t, "capture", nil)
	serve = session.Report(t, serve.Instance, map[string]any{
		"body":        "A tactical gap carrying a stray actor kind.",
		"entryKind":   "gap",
		"layer":       "tactical",
		"actorKind":   "human",
		"confidence":  "medium",
		"widenReport": "widened",
	})
	proctest.RequireStep(t, serve, "assemble")
	if !hasDiagnostic(serve, model.ActorKindScopeRule) {
		t.Fatalf("an actor kind on a non-actor should hold at assemble naming the scope rule, diagnostics=%v", serve.Diagnostics)
	}
}

func TestCapture_PlaybackAdjustCorrectsTheActorKind(t *testing.T) {
	world, session := newIdentityWorld(t, "identity-actor-kind-adjust")
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance

	draft := actorDraft()
	draft["actorKind"] = "human"
	session.Report(t, instance, draft)
	serve = session.Answer(t, instance, "playback", "adjust", map[string]any{"actorKind": "machine"}, "it's an assistant, not a person")
	proctest.RequireStep(t, serve, "playback")
	if !strings.Contains(serve.Instructions, "actorKind: machine") {
		t.Errorf("playback after an adjust should render the corrected actor kind, got %q", serve.Instructions)
	}

	serve = session.Answer(t, instance, "playback", "confirm", nil, "yes")
	proctest.RequireStep(t, serve, "verifySummary")
	serve = session.Answer(t, instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	proctest.RequireStatus(t, serve, "completed")
	entryID, _ := serve.Produced["entryId"].(string)
	if entry := proctest.LoadEntry(t, world.GraphDir, entryID); entry.ActorKind != model.ActorKindMachine {
		t.Errorf("the adjusted actor kind should persist, got %q", entry.ActorKind)
	}
}

const classifiedActorID = "20260601-110000-s-prc-cls"

// startReclassifyingCapture drafts a supersede of an actor declared as a
// machine that declares it human, confirms it, and returns at the hold the
// chain stability rule raises before write.
func startReclassifyingCapture(t *testing.T, connID string) (*proctest.Session, string) {
	t.Helper()
	world := proctest.NewWorld(t, proctest.WithEntries(append(captureFixtureEntries(),
		&model.Entry{
			ID: identityActorID, Type: model.TypeSignal, Kind: model.KindActor, Layer: model.LayerProcess, Canonical: "Christopher",
			Summary: "Christopher, the session's participant.", Content: "Christopher, the session's participant.",
		},
		&model.Entry{
			ID: classifiedActorID, Type: model.TypeSignal, Kind: model.KindActor, Layer: model.LayerProcess,
			Canonical: "Helper", ActorKind: model.ActorKindMachine,
			Summary: "Helper, an assistant declared as a machine.",
			Content: "Helper, an assistant declared as a machine.",
		})...))
	session := world.Open(t, connID)
	session.LogRead(t, "show", []string{captureRefID, classifiedActorID}, nil)
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance

	serve = session.Report(t, instance, map[string]any{
		"body":        "Helper is the coding assistant that drafts changes for review.",
		"entryKind":   "actor",
		"layer":       "process",
		"canonical":   "Helper",
		"actorKind":   "human",
		"supersedes":  []any{classifiedActorID},
		"confidence":  "high",
		"widenReport": "read the existing Helper actor before superseding it",
	})
	proctest.RequireStep(t, serve, "playback")
	serve = session.Answer(t, instance, "playback", "confirm", nil, "go")
	proctest.RequireStep(t, serve, "reviseOrOverride")
	if !strings.Contains(serve.Instructions, "actor_kind") || !strings.Contains(serve.Instructions, "contradicts") {
		t.Fatalf("the hold should name the actor kind contradicting its chain, got %q", serve.Instructions)
	}
	return session, instance
}

// TestCapture_ActorKindReclassificationHeldAtPreflight pins the chain
// stability rule on the engine path: a supersede contradicting the sort its
// chain declares is held before write, and the revise path can correct it.
func TestCapture_ActorKindReclassificationHeldAtPreflight(t *testing.T) {
	session, instance := startReclassifyingCapture(t, "identity-actor-kind-reclassify")

	serve := session.Answer(t, instance, "reviseOrOverride", "revise", map[string]any{"actorKind": "machine"}, "keep it a machine")
	proctest.RequireStep(t, serve, "playback")
	serve = session.Answer(t, instance, "playback", "confirm", nil, "go")
	proctest.RequireStep(t, serve, "verifySummary")
}

func TestCapture_ActorKindWrittenAfterPreflightReruns(t *testing.T) {
	session, instance := startReclassifyingCapture(t, "identity-actor-kind-stale-preflight")

	session.Report(t, instance, map[string]any{"actorKind": "human"})
	serve := session.Answer(t, instance, "reviseOrOverride", "override", nil, "write it")
	proctest.RequireStep(t, serve, "reviseOrOverride")
}

func TestReportSchema_AdvertisesActorKindSorts(t *testing.T) {
	_, session := newIdentityWorld(t, "identity-actor-kind-schema")
	serve := session.Start(t, "capture", nil)
	props, _ := serve.ReportSchema["properties"].(map[string]any)
	field, _ := props["actorKind"].(map[string]any)
	if field == nil {
		t.Fatalf("assemble should advertise actorKind, got %v", props)
	}
	enum, _ := nullableBranch(t, field, "string")["enum"].([]any)
	values := model.ActorKindValues()
	if len(enum) != len(values) {
		t.Fatalf("actorKind should advertise exactly the actor sorts, got %v", enum)
	}
	for i, v := range values {
		if enum[i] != string(v) {
			t.Errorf("actorKind should advertise the actor sorts in canonical order, got %v", enum)
		}
	}
}

func TestCapture_GuideReviewCorrectsTheActorKind(t *testing.T) {
	for _, choice := range []string{"revise", "recheck"} {
		t.Run(choice, func(t *testing.T) {
			world, session := newIdentityWorld(t, "identity-actor-kind-guide-"+choice)
			world.LLM.GuideFindings = []proctest.GuideFinding{{
				Reasoning: "two identities fused", Axis: "conflation",
				Quote: "CEO of networkteam", Repair: "split", Severity: "substantive",
			}}
			serve := session.Start(t, "capture", nil)
			instance := serve.Instance

			draft := actorDraft()
			draft["actorKind"] = "machine"
			serve = session.Report(t, instance, draft)
			proctest.RequireStep(t, serve, "guideReview")

			world.LLM.GuideFindings = nil
			serve = session.Answer(t, instance, "guideReview", choice, map[string]any{"actorKind": "human"}, "")
			proctest.RequireStep(t, serve, "playback")
			if !strings.Contains(serve.Instructions, "actorKind: human") {
				t.Errorf("playback after a guide review should render the corrected actor kind, got %q", serve.Instructions)
			}
		})
	}
}
