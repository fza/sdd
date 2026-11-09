package presenters_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// A bypassed entry names its grounding bypass in the show envelope
// (20261109-004506-d-cpt-udz, D4).
func TestRenderShow_EnvelopeNamesAGroundingBypass(t *testing.T) {
	e := entry("20260410-100000-s-tac-skp", withContent("An ungrounded gap."), withConfidence("low"))
	e.Grounding = model.GroundingSkipped
	g := model.NewGraph([]*model.Entry{e})
	if got := renderShow(t, g, []string{e.ID}); !strings.Contains(got, "grounding: skipped") {
		t.Fatalf("the envelope should name the bypass:\n%s", got)
	}
}
