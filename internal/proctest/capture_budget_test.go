package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	"github.com/networkteam/sdd/pkg/application/types"
)

// The playback serves within the capture's serve budget at its worst case.
func TestCapture_PlaybackFitsTheServeBudget(t *testing.T) {
	world := proctest.NewWorld(t)
	result, err := world.App.Lint(t.Context(), world.Identity, "", types.LintQuery{})
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range result.Findings {
		if finding.Code == "serve-budget" && finding.EntryID == "20260703-094500-d-prc-cap" && strings.Contains(finding.Message, `step "playback"`) {
			t.Errorf("the playback should fit the capture's serve budget: %s", finding.Message)
		}
	}
}
