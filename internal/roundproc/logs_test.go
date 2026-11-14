package roundproc_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/roundproc"
)

// A started process writes its output and its errors to its log, which Start
// creates with its directory.
func TestStartWritesTheOutputToTheLog(t *testing.T) {
	log := filepath.Join(t.TempDir(), "rounds", "github.com", "acme", "app", "child-round-1.log")

	process, err := roundproc.Start(t.TempDir(), log, "/bin/sh", "-c", "echo to stdout; echo to stderr >&2")
	if err != nil {
		t.Fatal(err)
	}

	eventually(t, "the process ending", func() bool {
		alive, err := roundproc.Alive(process)
		return err == nil && !alive
	})
	written, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "to stdout") || !strings.Contains(string(written), "to stderr") {
		t.Fatalf("the log should hold the process's output and errors, got %q", written)
	}
}

func TestLogPath(t *testing.T) {
	if got := roundproc.LogPath("/state/sdd/rounds/github.com/acme/app", "child", 2); got != "/state/sdd/rounds/github.com/acme/app/child-round-2.log" {
		t.Fatalf("LogPath() = %s", got)
	}
}

// Pruning removes the round logs unchanged for longer than the retention, in
// every repository's directory, and keeps the rest.
func TestPruneLogs(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	retention := 90 * 24 * time.Hour
	old := filepath.Join(root, "github.com", "acme", "app", "child-round-1.log")
	fresh := filepath.Join(root, "github.com", "acme", "app", "child-round-2.log")
	other := filepath.Join(root, "github.com", "acme", "app", "notes.txt")
	for _, path := range []string{old, fresh, other} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("written"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	long := now.Add(-retention - time.Hour)
	for _, path := range []string{old, other} {
		if err := os.Chtimes(path, long, long); err != nil {
			t.Fatal(err)
		}
	}

	removed, freed, err := roundproc.PruneLogs(root, now, retention)

	if err != nil || removed != 1 || freed != int64(len("written")) {
		t.Fatalf("pruning should remove the one expired log and count its bytes, got %d, %d, %v", removed, freed, err)
	}
	for path, kept := range map[string]bool{old: false, fresh: true, other: true} {
		if _, err := os.Stat(path); (err == nil) != kept {
			t.Errorf("%s kept should be %v, stat says %v", path, kept, err)
		}
	}
}

// A machine that never ran a round has no logs to prune.
func TestPruneLogsWithoutLogs(t *testing.T) {
	removed, freed, err := roundproc.PruneLogs(filepath.Join(t.TempDir(), "rounds"), time.Now(), time.Hour)

	if err != nil || removed != 0 || freed != 0 {
		t.Fatalf("pruning no logs should remove nothing, got %d, %d, %v", removed, freed, err)
	}
}
