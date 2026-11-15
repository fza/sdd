package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/cacheprune"
	"github.com/networkteam/sdd/internal/model"
)

func TestCachePruneCommandReportsItsFailureAsItsError(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, "config/sdd/config.yaml", "participant: Global Author\ncache:\n  retention: soon\n")
	if r := f.run(t, "cache", "prune"); r.err == nil || !strings.Contains(r.err.Error(), "cache.retention") {
		t.Fatalf("a run on an invalid retention should fail with its cause, got %v", r.err)
	}
	if next := f.run(t, "show", appEntryID); strings.Contains(next.stderr, "cache pruning") {
		t.Errorf("a later command should not repeat the run's failure, stderr:\n%s", next.stderr)
	}
}

func TestInfoShowsAFailedCachePrune(t *testing.T) {
	f := newAppFixture(t)
	stateRoot := filepath.Join(f.root, "state", "sdd")
	err := cacheprune.Update(t.Context(), stateRoot, func(s *model.CachePruneStatus) {
		s.StartedAt, s.FinishedAt, s.Error = time.Now(), time.Now(), "removing clone: permission denied"
	})
	if err != nil {
		t.Fatal(err)
	}
	if next := f.run(t, "show", appEntryID); next.err != nil || strings.Contains(next.stderr, "permission denied") {
		t.Errorf("a recorded failure should neither fail nor reach another command, got %v, stderr:\n%s", next.err, next.stderr)
	}
	if info := f.run(t, "info"); !strings.Contains(info.stdout, "Cache pruning: failed") || !strings.Contains(info.stdout, "permission denied") {
		t.Errorf("sdd info should show the failed run, stdout:\n%s", info.stdout)
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

func TestConfigSetIndexReplacedRetention(t *testing.T) {
	f := newAppFixture(t)
	if r := f.run(t, "config", "set", "index.replaced_retention", "7d"); r.err != nil {
		t.Errorf("a valid retention should be accepted globally: %v\n%s", r.err, r.stderr)
	}
	if r := f.run(t, "config", "set", "index.replaced_retention", "later"); r.err == nil || !strings.Contains(r.err.Error(), "positive duration") {
		t.Error("an invalid retention should be refused")
	}
	if r := f.run(t, "config", "set", "--local", "index.replaced_retention", "7d"); r.err == nil || !strings.Contains(r.err.Error(), "not a valid per-repo config key") {
		t.Error("a per-repo retention should be refused: the index is machine-global")
	}
}
