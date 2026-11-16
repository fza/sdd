// The capture's report schemas carry only the fields a report fills there.
package proctest_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
)

func TestCaptureSchemasCarryOnlyFieldsAReportFills(t *testing.T) {
	_, session := newCaptureWorld(t, "capture-schema")
	serve := session.Start(t, "capture", nil)
	proctest.RequireStep(t, serve, "assemble")
	props, _ := serve.ReportSchema["properties"].(map[string]any)
	for _, absent := range []string{"deepFindings", "deepResponses", "lateVerdicts", "deepLaunchFailure", "fidelityNote", "procedureSpec"} {
		if _, ok := props[absent]; ok {
			t.Errorf("the assemble schema should leave out %s", absent)
		}
	}
	if _, ok := props["body"]; !ok {
		t.Fatal("the assemble schema should carry the draft fields")
	}

	serve = session.Start(t, "capture", map[string]any{"kind": "procedure"})
	props, _ = serve.ReportSchema["properties"].(map[string]any)
	if _, ok := props["procedureSpec"]; !ok {
		t.Error("a procedure capture's assemble schema should carry procedureSpec")
	}
}
