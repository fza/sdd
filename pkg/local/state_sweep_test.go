package local_test

import (
	"encoding/json"
	"fmt"
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

func (f stateFixture) sweep(t *testing.T) (removed struct{ Sessions, Unreadable int }) {
	t.Helper()
	counts, err := localadapter.StateSweep{StateRoot: f.root}.Sweep(t.Context(), f.now, sweepSessionRetention, sweepUnusedRetention)
	if err != nil {
		t.Fatalf("sweeping: %v", err)
	}
	removed.Sessions, removed.Unreadable = counts.Sessions, counts.Unreadable
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

// An ended session goes once sessions.retention passed since it ended, with
// its staged attachments, in every project; open sessions and unreadable logs
// stay.
func TestStateSweepCollectsSessions(t *testing.T) {
	f := newStateFixture(t)
	const key, other = "github.com/acme/app", "local/abcdef012345"
	f.session(t, key, "s_old", f.now.Add(-sweepSessionRetention-time.Hour))
	f.session(t, key, "s_recent", f.now.Add(-sweepSessionRetention+time.Hour))
	f.session(t, key, "s_open", time.Time{})
	f.session(t, other, "s_other", f.now.Add(-sweepSessionRetention-time.Hour))
	unreadable := filepath.Join(f.location(key).Sessions, "s_unreadable.jsonl")
	if err := os.WriteFile(unreadable, []byte("{not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	removed := f.sweep(t)

	if removed.Sessions != 2 || removed.Unreadable != 1 {
		t.Errorf("the sweep should remove two ended sessions and report one unreadable log, got %+v", removed)
	}
	for id, wantKept := range map[sdd.SessionID]bool{"s_old": false, "s_recent": true, "s_open": true} {
		if kept := f.logExists(t, key, id); kept != wantKept {
			t.Errorf("session %s kept should be %v", id, wantKept)
		}
		if kept := f.stagedExists(t, key, id); kept != wantKept {
			t.Errorf("the staged attachments of %s kept should be %v", id, wantKept)
		}
	}
	if f.logExists(t, other, "s_other") {
		t.Error("the sweep should collect the sessions of every project under the state root")
	}
	if _, err := os.Stat(unreadable); err != nil {
		t.Errorf("an unreadable log may belong to a newer binary and should stay: %v", err)
	}
}

// Collection pages through a project's sessions, so it reaches past the first
// page.
func TestStateSweepCollectsPastTheFirstPage(t *testing.T) {
	f := newStateFixture(t)
	const key = "github.com/acme/app"
	for i := range 205 {
		f.session(t, key, sdd.SessionID(fmt.Sprintf("s_%03d", i)), f.now.Add(-sweepSessionRetention-time.Hour))
	}

	removed := f.sweep(t)

	if removed.Sessions != 205 {
		t.Errorf("the sweep should collect every ended session across pages, removed %d", removed.Sessions)
	}
}
