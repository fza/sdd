package gitgate_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/gitgate"
)

func TestPruneRecords(t *testing.T) {
	const retention = 90 * 24 * time.Hour
	s := newSetup(t)
	gate := s.gate()
	stale := mustTarget(t, "git@gitlab.com:group/stale.git")
	fresh := mustTarget(t, "git@gitlab.com:group/fresh.git")
	leased := mustTarget(t, "git@gitlab.com:group/leased.git")
	runs := 0
	if err := gate.Run(t.Context(), stale, gitgate.Fetch, sshOp(t, &runs)); err != nil {
		t.Fatal(err)
	}
	s.clock.Set(t0.Add(30 * 24 * time.Hour))
	if _, err := gate.Demand(t.Context(), leased); err != nil {
		t.Fatal(err)
	}
	if err := gate.Run(t.Context(), fresh, gitgate.Fetch, sshOp(t, &runs)); err != nil {
		t.Fatal(err)
	}

	now := t0.Add(retention + time.Hour)
	removed, err := gate.PruneRecords(t.Context(), now, retention)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Errorf("only the record unused for the retention should go, a lease counting as use, removed %d", removed)
	}
	for target, wantKept := range map[gitgate.Target]bool{stale: false, fresh: true} {
		last, err := gate.LastAttempt(t.Context(), target)
		if err != nil {
			t.Fatal(err)
		}
		if kept := !last.IsZero(); kept != wantKept {
			t.Errorf("the record of %s kept should be %v", target.Repo, wantKept)
		}
	}
}

func TestSweepTraces(t *testing.T) {
	const minAge = 24 * time.Hour
	s := newSetup(t)
	dir := filepath.Join(s.dir, "traces")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	old, young := filepath.Join(dir, "1.json"), filepath.Join(dir, "2.json")
	for _, path := range []string{old, young} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	past := now.Add(-minAge - time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	removed, err := s.gate().SweepTraces(now, minAge)
	if err != nil || removed != 1 {
		t.Fatalf("one trace should go, removed %d: %v", removed, err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("a trace older than a day should go")
	}
	if _, err := os.Stat(young); err != nil {
		t.Error("a trace of an operation that may still run should stay")
	}
}
