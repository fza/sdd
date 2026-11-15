package model_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func gateReading() *model.Reading {
	var r model.Reading
	r.Fold(model.ReadPayload{
		Reads:  []model.GroundingRead{{Mode: model.GroundingSemantic, Text: "q", IDs: []string{"a"}}, {Mode: model.GroundingKeyword, Text: "t", IDs: []string{"b"}}},
		Full:   []string{"b"},
		Corpus: model.NewCorpusMark([]string{"a", "b"}),
	})
	return &r
}

func TestGroundingEvidence_OwedReads(t *testing.T) {
	topic, err := model.ParseTopicPath("cli/ux")
	if err != nil {
		t.Fatal(err)
	}
	owed := model.GroundingEvidence{Reading: gateReading(), Topics: []model.TopicPath{topic}}.OwedReads()
	if !slices.Equal(owed, []string{"the listing of topic cli/ux"}) {
		t.Fatalf("owed = %v, want the unlisted draft topic only", owed)
	}
	var briefs model.Reading
	briefs.Fold(model.ReadPayload{Reads: []model.GroundingRead{{Mode: model.GroundingBriefs, IDs: []string{"a"}}}})
	if owed := (model.GroundingEvidence{Reading: &briefs, Topics: []model.TopicPath{topic}}).OwedReads(); len(owed) != 0 {
		t.Fatalf("the all-briefs mode owes nothing, got %v", owed)
	}
}

func TestGroundingEvidence_FullReadsAndVerdicts(t *testing.T) {
	e := model.GroundingEvidence{Reading: gateReading()}
	if open := e.OpenFullReads(); !slices.Equal(open, []string{"b"}) {
		t.Fatalf("open = %v, want the entry served in full", open)
	}
	e.Verdicts = []model.GroundingVerdict{{IDs: []string{"b", "z"}, Code: model.VerdictCovered}}
	if open := e.OpenFullReads(); len(open) != 0 {
		t.Fatalf("a verdict should answer the full read, got %v", open)
	}
	if unlisted := e.UnlistedVerdicts(); !slices.Equal(unlisted, []string{"z"}) {
		t.Fatalf("unlisted = %v, want the verdict ID no read listed", unlisted)
	}
}

// Landed entries: listed ones pass the assemble check, a late read alone does
// not answer the write step, and the session's own entries never count.
func TestGroundingEvidence_LandedEntries(t *testing.T) {
	r := gateReading()
	e := model.GroundingEvidence{Reading: r, Live: []string{"a", "b", "new", "late", "own"}, Own: []string{"own"}}
	if got := e.Unlisted(); !slices.Equal(got, []string{"late", "new"}) {
		t.Fatalf("unlisted = %v, want the landed entries", got)
	}
	r.Fold(model.ReadPayload{Reads: []model.GroundingRead{{Mode: model.GroundingNew, IDs: []string{"new"}}}})
	r.Fold(model.ReadPayload{Reads: []model.GroundingRead{{Mode: model.GroundingLate, IDs: []string{"late"}}}})
	if got := e.Unlisted(); len(got) != 0 {
		t.Fatalf("every landed entry is listed now, got %v", got)
	}
	if got := e.Late(); !slices.Equal(got, []string{"late"}) {
		t.Fatalf("late = %v, want the entry only the late read listed", got)
	}
	e.Verdicts = []model.GroundingVerdict{{IDs: []string{"late"}, Code: model.VerdictOtherScope}}
	if got := e.Late(); len(got) != 0 {
		t.Fatalf("a verdict should answer the late entry, got %v", got)
	}
}

func TestWithReadingSeedReplacesTheSeedInPlace(t *testing.T) {
	r := gateReading()
	first := model.WithReadingSeed("inherited", r)
	r.Fold(model.ReadPayload{Reads: []model.GroundingRead{{Mode: model.GroundingOpened, IDs: []string{"c"}}}, Full: []string{"c"}})
	second := model.WithReadingSeed(first+"\n\nagent prose", r)
	if !strings.HasPrefix(second, "inherited\n\nReading recorded by sdd gather:") || !strings.HasSuffix(second, "agent prose") {
		t.Fatalf("the text around the seed should stay:\n%s", second)
	}
	if strings.Count(second, "Reading recorded by sdd gather:") != 1 || !strings.Contains(second, "served in full: b, c") {
		t.Fatalf("the seed should be replaced with the current reading:\n%s", second)
	}
}
