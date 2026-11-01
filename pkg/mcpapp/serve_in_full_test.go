package mcpapp_test

import (
	"strings"
	"testing"

	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// With ServeInFull the served-once memory is off (20261101-164201-d-tac-sj4):
// a second capture serves its lanes and report schema in full again.
func TestServeInFullServesRepeatsInFull(t *testing.T) {
	env := newTestServer(t, nil, t.TempDir(), "", func(o *testServerOptions) { o.MCP.ServeInFull = true })
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session

	var first mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "capture"}, &first)
	var second mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "capture"}, &second)

	if !laneTextContains(second.Lanes, captureIntroProse) {
		t.Errorf("a second capture should serve its intro lane in full again, got %v", second.Lanes)
	}
	if laneTextContains(second.Lanes, withheldLanesPrefix) {
		t.Errorf("no lane should be withheld, got %v", second.Lanes)
	}
	requireFullReportSchema(t, "second capture", second.ReportSchema)
	if first.FramingBlocks != nil && len(*first.FramingBlocks) > 0 && (second.FramingBlocks == nil || len(*second.FramingBlocks) == 0) {
		t.Error("a later serve should carry the framing again")
	}
}

func laneTextContains(lanes *[]mcpserver.NamedText, text string) bool {
	if lanes == nil {
		return false
	}
	for _, lane := range *lanes {
		if strings.Contains(lane.Text, text) {
			return true
		}
	}
	return false
}
