package local_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

const (
	sweepSessionRetention = 14 * 24 * time.Hour
	sweepUnusedRetention  = 90 * 24 * time.Hour
)

// stateFixture is a state root holding projects keyed like index.RepoKey.
type stateFixture struct {
	root string
	now  time.Time
}

func newStateFixture(t *testing.T) stateFixture {
	t.Helper()
	return stateFixture{root: t.TempDir(), now: time.Now().UTC().Round(0)}
}

func (f stateFixture) location(key string) localadapter.StoreLocation {
	return localadapter.StoreLocation{
		Name:        key,
		Sessions:    filepath.Join(f.root, "sessions", filepath.FromSlash(key)),
		StagedBlobs: filepath.Join(f.root, "staged-blobs", filepath.FromSlash(key)),
		Subject:     "local",
		Project:     sdd.ProjectID(key),
	}
}

// session writes a session of the project under key, ended at ended unless
// that is zero, with one staged attachment.
func (f stateFixture) session(t *testing.T, key string, id sdd.SessionID, ended time.Time) {
	t.Helper()
	location := f.location(key)
	sessions, err := localadapter.NewFilesystemSessionStore(location)
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := localadapter.NewFilesystemStagedBlobStore(location)
	if err != nil {
		t.Fatal(err)
	}
	metadata := sdd.SessionMetadata{ID: id, Subject: "local", Project: location.Project, Participant: "Christopher", UpdatedAt: f.now.Add(-200 * 24 * time.Hour)}
	created, err := sessions.Create(t.Context(), metadata)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := blobs.Stage(t.Context(), sdd.SessionRef{Subject: "local", Session: id}, "evidence.md", strings.NewReader("evidence")); err != nil {
		t.Fatal(err)
	}
	if ended.IsZero() {
		return
	}
	metadata.Ended = &sdd.SessionEnd{Act: sdd.SessionConcluded, EndedAt: ended}
	payload, err := json.Marshal(metadata.Ended)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Append(t.Context(), id, created.Version, sdd.SessionAppend{Metadata: &metadata, Events: []sdd.StoredEvent{{CodecVersion: sdd.SessionCodecVersion, Code: sdd.SessionEndedEventCode, Payload: payload}}}); err != nil {
		t.Fatal(err)
	}
}

func (f stateFixture) relevance(t *testing.T, key string) string {
	t.Helper()
	dir := filepath.Join(f.root, "relevance", filepath.FromSlash(key))
	store, err := localadapter.NewFilesystemRelevanceStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Append(t.Context(), []byte(`{"verdicts":[]}`)); err != nil {
		t.Fatal(err)
	}
	return dir
}

func (f stateFixture) used(t *testing.T, key string, at time.Time) {
	t.Helper()
	if err := localadapter.MarkProjectUsed(f.root, key, at); err != nil {
		t.Fatal(err)
	}
}

func (f stateFixture) sweep(t *testing.T) (removed struct{ Sessions, Projects, Unreadable int }) {
	t.Helper()
	counts, err := localadapter.StateSweep{StateRoot: f.root}.Sweep(t.Context(), f.now, sweepSessionRetention, sweepUnusedRetention)
	if err != nil {
		t.Fatalf("sweeping: %v", err)
	}
	removed.Sessions, removed.Projects, removed.Unreadable = counts.Sessions, counts.Projects, counts.Unreadable
	return removed
}

func (f stateFixture) logExists(t *testing.T, key string, id sdd.SessionID) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(f.location(key).Sessions, string(id)+".jsonl"))
	return err == nil
}

func (f stateFixture) stagedExists(t *testing.T, key string, id sdd.SessionID) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(f.location(key).StagedBlobs, "local", string(id)))
	return err == nil
}

// Rule 2: an ended session goes once sessions.retention passed since it
// ended, with its staged attachments; open sessions and unreadable logs stay.
func TestStateSweepCollectsSessions(t *testing.T) {
	f := newStateFixture(t)
	const key = "github.com/acme/app"
	f.used(t, key, f.now)
	f.session(t, key, "s_old", f.now.Add(-sweepSessionRetention-time.Hour))
	f.session(t, key, "s_recent", f.now.Add(-sweepSessionRetention+time.Hour))
	f.session(t, key, "s_open", time.Time{})
	unreadable := filepath.Join(f.location(key).Sessions, "s_unreadable.jsonl")
	if err := os.WriteFile(unreadable, []byte("{not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	removed := f.sweep(t)

	if removed.Sessions != 1 || removed.Unreadable != 1 {
		t.Errorf("the sweep should remove one ended session and report one unreadable log, got %+v", removed)
	}
	for id, wantKept := range map[sdd.SessionID]bool{"s_old": false, "s_recent": true, "s_open": true} {
		if kept := f.logExists(t, key, id); kept != wantKept {
			t.Errorf("session %s kept should be %v", id, wantKept)
		}
		if kept := f.stagedExists(t, key, id); kept != wantKept {
			t.Errorf("the staged attachments of %s kept should be %v", id, wantKept)
		}
	}
	if _, err := os.Stat(unreadable); err != nil {
		t.Errorf("an unreadable log may belong to a newer binary and should stay: %v", err)
	}
}

// Rule 3: the relevance ledger and the empty session folder of a project
// unused for cache.retention go; open sessions and a used project stay.
func TestStateSweepRemovesTheStateOfUnusedProjects(t *testing.T) {
	f := newStateFixture(t)
	long := f.now.Add(-sweepUnusedRetention - time.Hour)
	const (
		unused     = "github.com/acme/unused"
		unusedOpen = "github.com/acme/paused"
		used       = "github.com/acme/used"
	)
	unusedLedger := f.relevance(t, unused)
	f.session(t, unused, "s_ended", long)
	f.used(t, unused, long)
	pausedLedger := f.relevance(t, unusedOpen)
	f.session(t, unusedOpen, "s_open", time.Time{})
	f.used(t, unusedOpen, long)
	usedLedger := f.relevance(t, used)
	f.used(t, used, f.now.Add(-sweepUnusedRetention+time.Hour))

	removed := f.sweep(t)

	if removed.Projects != 2 {
		t.Errorf("both unused projects should lose state, got %+v", removed)
	}
	for dir, wantKept := range map[string]bool{
		unusedLedger: false, f.location(unused).Sessions: false, f.location(unused).StagedBlobs: false,
		pausedLedger: false, usedLedger: true,
	} {
		if _, err := os.Stat(dir); (err == nil) != wantKept {
			t.Errorf("%s kept should be %v, stat says %v", dir, wantKept, err)
		}
	}
	if !f.logExists(t, unusedOpen, "s_open") || !f.stagedExists(t, unusedOpen, "s_open") {
		t.Error("an open session and its attachments stay however long the project was unused")
	}
}

func TestMarkProjectUsedRefreshesOnceADay(t *testing.T) {
	f := newStateFixture(t)
	const key = "local/abcdef012345"
	f.used(t, key, f.now.Add(-48*time.Hour))
	f.used(t, key, f.now.Add(-47*time.Hour))
	stamp := filepath.Join(f.root, "used", "local", "abcdef012345.stamp")
	info, err := os.Stat(stamp)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(f.now.Add(-48 * time.Hour)) {
		t.Error("a use within a day of the stamp should leave it alone")
	}
	f.used(t, key, f.now)
	if info, err := os.Stat(stamp); err != nil || !info.ModTime().Equal(f.now) {
		t.Errorf("a use a day after the stamp should refresh it, got %v, %v", info.ModTime(), err)
	}
}
