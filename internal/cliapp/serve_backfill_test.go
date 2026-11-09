package cliapp_test

import (
	"strings"
	"testing"
)

// sdd serve returns only once the title backfill a session start began has
// committed, so no write outlives the process.
func TestServeReturnsAfterItsTitleBackfill(t *testing.T) {
	f := sessionFixture(t, false)
	openFixtureSession(t, f)
	if subject := headSubject(t, f.root); !strings.HasPrefix(subject, "sdd: summarize") {
		t.Errorf("the backfill's commit should be in place when serve returns, head is %q", subject)
	}
}
