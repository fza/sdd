package model_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func actorWithKind(canonical string, kind model.ActorKind) *model.Entry {
	return &model.Entry{
		ID:        "20260925-120000-s-prc-aaa",
		Type:      model.TypeSignal,
		Kind:      model.KindActor,
		Layer:     model.LayerProcess,
		Canonical: canonical,
		ActorKind: kind,
		Content:   "actor " + canonical,
	}
}

// A consumer assigning ownership reads the field off the written entry, so it
// has to survive the round trip through frontmatter.
func TestActorKindSurvivesTheFrontmatterRoundTrip(t *testing.T) {
	written := model.FormatFrontmatter(actorWithKind("Felix", model.ActorKindHuman))
	if !strings.Contains(written, "actor_kind: human") {
		t.Fatalf("actor_kind missing from the written frontmatter:\n%s", written)
	}

	parsed, err := model.ParseEntry("20260925-120000-s-prc-aaa", written+"\nactor Felix\n")
	if err != nil {
		t.Fatalf("ParseEntry: %v", err)
	}
	if parsed.ActorKind != model.ActorKindHuman {
		t.Errorf("ActorKind = %q, want human", parsed.ActorKind)
	}
	if len(parsed.Warnings) > 0 {
		t.Errorf("a valid actor_kind must not warn: %+v", parsed.Warnings)
	}
}

// Absence is unknown, not human: nothing may read a person out of silence.
func TestActorKindIsOptional(t *testing.T) {
	entry := actorWithKind("Ada", "")
	written := model.FormatFrontmatter(entry)
	if strings.Contains(written, "actor_kind") {
		t.Errorf("an unset actor_kind must not be written:\n%s", written)
	}
	if _, err := model.ParseEntry(entry.ID, written+"\nactor Ada\n"); err != nil {
		t.Fatalf("ParseEntry: %v", err)
	}
}

func TestActorKindRefusesAnUnknownValue(t *testing.T) {
	construction, strays := model.ConstructFromEntry(actorWithKind("Ada", "robot"))
	if len(strays) > 0 {
		t.Fatalf("an actor carrying the field is not a stray: %+v", strays)
	}
	if findings := construction.Validate(nil); !findsField(findings, "actor_kind") {
		t.Errorf("an unknown actor_kind must be refused: %+v", findings)
	}
}

// The field answers a question only an actor poses, so it is a stray field
// anywhere else — the same rule aliases follow.
func TestActorKindIsRefusedOutsideActors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry *model.Entry
	}{
		{
			name: "gap signal",
			entry: &model.Entry{
				ID: "20260925-120000-s-tac-aaa", Type: model.TypeSignal, Kind: model.KindGap,
				Layer: model.LayerTactical, ActorKind: model.ActorKindHuman, Content: "a gap",
			},
		},
		{
			name: "procedure decision",
			entry: &model.Entry{
				ID: "20260925-120000-d-prc-aaa", Type: model.TypeDecision, Kind: model.KindProcedure,
				Layer: model.LayerProcess, Canonical: "capture", ActorKind: model.ActorKindMachine, Content: "a procedure",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, findings := model.ConstructFromEntry(tc.entry)
			if !findsField(findings, "actor_kind") {
				t.Errorf("actor_kind outside an actor must be refused: %+v", findings)
			}
		})
	}
}

func findsField(findings []model.Finding, field string) bool {
	for _, f := range findings {
		if strings.Contains(f.Field+" "+f.Message, field) {
			return true
		}
	}
	return false
}
