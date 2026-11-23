package roundproc_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
