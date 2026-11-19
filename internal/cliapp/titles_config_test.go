package cliapp_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigKnowsTitlesBackfill(t *testing.T) {
	f := newAppFixture(t)
	shown := func() string {
		t.Helper()
		for line := range strings.SplitSeq(f.run(t, "config").stdout, "\n") {
			if strings.Contains(line, "titles.backfill") {
				return strings.Join(strings.Fields(line), " ")
			}
		}
		return ""
	}

	if got := shown(); !strings.Contains(got, "false") || !strings.Contains(got, "default") {
		t.Errorf("sdd config should list titles.backfill as off by default, got %q", got)
	}

	committed := filepath.Join(".sdd", "config.yaml")
	f.write(t, committed, readFile(t, filepath.Join(f.root, committed))+titlesBackfillOn)
	if got := shown(); !strings.Contains(got, "true") || !strings.Contains(got, "project") {
		t.Errorf("the committed project config should switch titles.backfill on, got %q", got)
	}

	set := f.run(t, "config", "set", "--local", "titles.backfill", "false")
	if set.err != nil {
		t.Fatalf("sdd config set should accept titles.backfill: %v\n%s", set.err, set.stderr)
	}
	if got := shown(); !strings.Contains(got, "false") || !strings.Contains(got, "local") {
		t.Errorf("the local config should switch titles.backfill off for this checkout, got %q", got)
	}
	if got := strings.TrimSpace(f.run(t, "config", "get", "titles.backfill").stdout); got != "false" {
		t.Errorf("sdd config get should print the effective value, got %q", got)
	}
}
