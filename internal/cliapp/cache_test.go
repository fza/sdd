package cliapp_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/cacheprune"
	"github.com/networkteam/sdd/internal/cliapp"
	"github.com/networkteam/sdd/internal/model"
)

// runWithPrune runs one command with a recording launcher in place of the
// detached process.
func (f *appFixture) runWithPrune(t *testing.T, launched *[]string, args ...string) appResult {
	t.Helper()
	var stdout, stderr bytes.Buffer
	command := cliapp.New(cliapp.Options{
		Version: appVersion, Reader: strings.NewReader(""), Writer: &stdout, ErrWriter: &stderr,
		LaunchCachePrune: func(dir, _ string) error { *launched = append(*launched, dir); return nil },
	})
	err := command.Run(t.Context(), append([]string{"sdd"}, args...))
	return appResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

func TestCommandsLaunchTheDailyCachePruneOnce(t *testing.T) {
	f := newAppFixture(t)
	var launched []string
	for range 3 {
		if r := f.runWithPrune(t, &launched, "show", appEntryID); r.err != nil {
			t.Fatalf("show: %v\n%s", r.err, r.stderr)
		}
	}
	if len(launched) != 1 || launched[0] != f.root {
		t.Errorf("three commands should launch one pass from the working directory, launched %v", launched)
	}
}

func TestCachePruneLaunchesNoSecondPass(t *testing.T) {
	f := newAppFixture(t)
	var launched []string
	if r := f.runWithPrune(t, &launched, "cache", "prune"); r.err != nil {
		t.Fatalf("cache prune: %v\n%s", r.err, r.stderr)
	}
	if len(launched) != 0 {
		t.Errorf("sdd cache prune is the pass itself and should launch none, launched %v", launched)
	}
	if r := f.runWithPrune(t, &launched, "show", appEntryID); r.err != nil {
		t.Fatalf("show: %v\n%s", r.err, r.stderr)
	}
	if len(launched) != 0 {
		t.Errorf("a pass run by hand counts as the day's pass, launched %v", launched)
	}
}

func TestCommandsReportAFailedCachePruneOnce(t *testing.T) {
	f := newAppFixture(t)
	stateRoot := filepath.Join(f.root, "state", "sdd")
	err := cacheprune.Update(t.Context(), stateRoot, func(s *model.CachePruneStatus) {
		s.LaunchedAt, s.FinishedAt, s.Error = time.Now(), time.Now(), "removing clone: permission denied"
	})
	if err != nil {
		t.Fatal(err)
	}
	var launched []string
	first := f.runWithPrune(t, &launched, "show", appEntryID)
	second := f.runWithPrune(t, &launched, "show", appEntryID)
	if first.err != nil || second.err != nil {
		t.Fatalf("a failed pass must not fail the command: %v, %v", first.err, second.err)
	}
	if !strings.Contains(first.stderr, "permission denied") {
		t.Errorf("the first command should report the failed pass, stderr:\n%s", first.stderr)
	}
	if strings.Contains(second.stderr, "permission denied") {
		t.Errorf("the failure should be reported once, second stderr:\n%s", second.stderr)
	}
	info := f.run(t, "info", "--format", "json")
	var parsed struct {
		CachePrune struct{ Error string } `json:"cache_prune"`
	}
	if err := json.Unmarshal([]byte(info.stdout), &parsed); err != nil {
		t.Fatalf("info JSON: %v\n%s", err, info.stdout)
	}
	if parsed.CachePrune.Error == "" {
		t.Errorf("sdd info should keep showing the failed pass, got %s", info.stdout)
	}
}

func TestCachePruneCommand(t *testing.T) {
	f := newAppFixture(t)
	clone := filepath.Join(f.root, "cache", "sdd", "github.com", "acme", "dropped", ".git")
	if err := os.MkdirAll(clone, 0o755); err != nil {
		t.Fatal(err)
	}
	r := f.run(t, "cache", "prune")
	if r.err != nil {
		t.Fatalf("cache prune: %v\n%s", r.err, r.stderr)
	}
	if !strings.Contains(r.stdout, "1 clones") {
		t.Errorf("the run should name what it removed, stdout:\n%s", r.stdout)
	}
	if _, err := os.Stat(clone); !os.IsNotExist(err) {
		t.Errorf("the clone no connection names should be gone, stat says %v", err)
	}
	info := f.run(t, "info")
	if !strings.Contains(info.stdout, "Cache pruning: last run") {
		t.Errorf("sdd info should name the last pass, stdout:\n%s", info.stdout)
	}
}

func TestConfigSetCacheRetention(t *testing.T) {
	f := newAppFixture(t)
	if r := f.run(t, "config", "set", "cache.retention", "30d"); r.err != nil {
		t.Errorf("a valid retention should be accepted globally: %v\n%s", r.err, r.stderr)
	}
	if r := f.run(t, "config", "set", "cache.retention", "soon"); r.err == nil || !strings.Contains(r.err.Error(), "positive duration") {
		t.Error("an invalid retention should be refused")
	}
	if r := f.run(t, "config", "set", "--local", "cache.retention", "30d"); r.err == nil || !strings.Contains(r.err.Error(), "not a valid per-repo config key") {
		t.Error("a per-repo retention should be refused: the cache is machine-global")
	}
}
