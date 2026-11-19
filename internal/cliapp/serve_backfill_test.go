package cliapp_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// Where titles.backfill is on, sdd serve returns only once the title backfill
// a session start began has committed, so no write outlives the process;
// elsewhere a session start titles nothing.
func TestServeBackfillsTitlesAtSessionStartOnlyWhereSwitchedOn(t *testing.T) {
	for _, tc := range []struct {
		name     string
		switched string
		titled   bool
	}{
		{name: "switched on", switched: titlesBackfillOn, titled: true},
		{name: "not switched on"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := sessionFixture(t, false)
			local := filepath.Join(".sdd", "config.local.yaml")
			f.write(t, local, readFile(t, filepath.Join(f.root, local))+tc.switched)

			openFixtureSession(t, f)

			subject := headSubject(t, f.root)
			if strings.HasPrefix(subject, "sdd: summarize") != tc.titled {
				t.Errorf("the backfill's commit should be in place when serve returns only with the switch on, head is %q", subject)
			}
		})
	}
}
