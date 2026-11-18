package local_test

import (
	"testing"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

// One run holds the directory at a time; another holder, in this process or
// another, reads its record until it releases.
func TestFilesystemCalibrationRuns_AdmitsOneRunAndSharesItsRecord(t *testing.T) {
	dir := t.TempDir() + "/relevance/repo"
	runs, err := localadapter.NewFilesystemCalibrationRuns(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, running, err := runs.Running(t.Context()); err != nil || running {
		t.Fatalf("running = %v, %v: a fresh directory holds no run", running, err)
	}
	lease, ok, err := runs.Acquire(t.Context())
	if err != nil || !ok {
		t.Fatalf("acquire = %v, %v: the first run should be admitted", ok, err)
	}
	other, err := localadapter.NewFilesystemCalibrationRuns(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := other.Acquire(t.Context()); err != nil || ok {
		t.Fatalf("second acquire = %v, %v: a held run admits no other", ok, err)
	}
	want := sdd.CalibrationRun{Judge: "chat:j", Started: time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC), Stage: 0.9, Scored: 42, Waiting: true}
	if err := lease.Record(t.Context(), want); err != nil {
		t.Fatal(err)
	}
	got, running, err := other.Running(t.Context())
	if err != nil || !running || got != want {
		t.Fatalf("running = %+v, %v, %v: another holder should read the record", got, running, err)
	}
	if err := lease.Release(); err != nil {
		t.Fatal(err)
	}
	if _, running, err := other.Running(t.Context()); err != nil || running {
		t.Fatalf("running = %v, %v: a released run is over", running, err)
	}
	again, ok, err := other.Acquire(t.Context())
	if err != nil || !ok {
		t.Fatalf("acquire after release = %v, %v", ok, err)
	}
	if err := again.Release(); err != nil {
		t.Fatal(err)
	}
}
