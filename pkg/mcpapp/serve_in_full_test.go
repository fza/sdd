package mcpapp_test

import (
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

	requireInstructionsContain(t, second, captureIntroProse)
	requireNoWithheldLanes(t, second)
	requireFullReportSchema(t, "second capture", second.ReportSchema)
	if first.Framing != "" && second.Framing == "" {
		t.Error("a later serve should carry the framing again")
	}
}
