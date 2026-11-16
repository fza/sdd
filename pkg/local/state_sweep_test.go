package local_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/flock"

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

func (f stateFixture) used(t *testing.T, key string, at time.Time) {
	t.Helper()
	if err := localadapter.MarkProjectUsed(f.root, key, at); err != nil {
		t.Fatal(err)
	}
}

// The session folder and staged attachments of a project unused for
// cache.retention go; open sessions, sessions not yet collected, a held lock
// and attachments a session elsewhere owns stay.
func TestStateSweepRemovesTheStateOfUnusedProjects(t *testing.T) {
	f := newStateFixture(t)
	long := f.now.Add(-sweepUnusedRetention - time.Hour)
	const (
		unused     = "github.com/acme/unused"
		unusedOpen = "github.com/acme/paused"
		used       = "github.com/acme/used"
		retained   = "github.com/acme/retained"
		unstamped  = "github.com/acme/unstamped"
		locked     = "github.com/acme/locked"
		legacy     = "github.com/acme/legacy"
		usedEmpty  = "github.com/acme/usedempty"
	)
	f.session(t, unused, "s_ended", long)
	f.used(t, unused, long)
	f.session(t, retained, "s_recent", f.now.Add(-time.Hour))
	f.used(t, retained, long)
	f.session(t, unstamped, "s_fresh", time.Time{})
	lockDir := filepath.Join(f.root, "sessions", filepath.FromSlash(locked))
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	held := flock.New(filepath.Join(lockDir, "s_gone.lock"))
	if _, err := held.TryLock(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Unlock() }()
	f.used(t, locked, long)
	// s_open is open in another project, so it owns these attachments.
	legacyArea := filepath.Join(f.location(legacy).StagedBlobs, "local", "s_open")
	if err := os.MkdirAll(legacyArea, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyArea, "0123456789abcdef0123456789abcdef.blob"), []byte("evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.used(t, legacy, long)
	f.session(t, unusedOpen, "s_open", time.Time{})
	f.used(t, unusedOpen, long)
	f.session(t, used, "s_used", time.Time{})
	f.used(t, used, f.now.Add(-sweepUnusedRetention+time.Hour))
	// Used recently, with no session left and attachments no session owns.
	usedEmptyArea := filepath.Join(f.location(usedEmpty).StagedBlobs, "local", "s_gone")
	for _, dir := range []string{usedEmptyArea, f.location(usedEmpty).Sessions} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := localadapter.RecordCheckout(f.root, usedEmpty, t.TempDir(), f.now); err != nil {
		t.Fatal(err)
	}
	f.used(t, usedEmpty, f.now.Add(-time.Hour))

	removed := f.sweep(t)

	if removed.Projects != 1 {
		t.Errorf("the one unused project without sessions should lose its state, got %+v", removed)
	}
	for dir, wantKept := range map[string]bool{
		f.location(unused).Sessions: false, f.location(unused).StagedBlobs: false,
		f.location(used).Sessions: true, f.location(unstamped).Sessions: true, lockDir: true, legacyArea: true,
		f.location(usedEmpty).Sessions: true, usedEmptyArea: true,
	} {
		if _, err := os.Stat(dir); (err == nil) != wantKept {
			t.Errorf("%s kept should be %v, stat says %v", dir, wantKept, err)
		}
	}
	if _, err := os.Stat(filepath.Join(f.root, "used", "github.com", "acme", "unused.stamp")); err == nil {
		t.Error("a retired project's use stamp should go with its state")
	}
	if !f.logExists(t, unusedOpen, "s_open") || !f.stagedExists(t, unusedOpen, "s_open") {
		t.Error("an open session and its attachments stay however long the project was unused")
	}
	if !f.logExists(t, retained, "s_recent") || !f.stagedExists(t, retained, "s_recent") {
		t.Error("an ended session not yet collected keeps its attachments however long the project was unused")
	}
	if _, err := os.Stat(filepath.Join(f.root, "used", "github.com", "acme", "retained.stamp")); err != nil {
		t.Errorf("the use stamp should stay while a session remains: %v", err)
	}
}

// A project whose last session ended without using it, as a long-running
// server or a cache.retention below sessions.retention allows, loses its
// session folder on the sweep that collects that session.
func TestStateSweepRetiresAnUnusedProjectOnceItsLastSessionIsCollected(t *testing.T) {
	f := newStateFixture(t)
	const key = "github.com/acme/unused"
	f.used(t, key, f.now.Add(-sweepUnusedRetention-time.Hour))
	f.session(t, key, "s_ended", f.now.Add(-sweepSessionRetention+time.Hour))

	f.sweep(t)
	if !f.logExists(t, key, "s_ended") {
		t.Fatal("an ended session within sessions.retention should stay")
	}
	later := f
	later.now = f.now.Add(2 * time.Hour)
	removed := later.sweep(t)

	if removed.Sessions != 1 || removed.Projects != 1 {
		t.Errorf("the later sweep should collect the session and retire the project, got %+v", removed)
	}
	if _, err := os.Stat(f.location(key).Sessions); err == nil {
		t.Error("the session folder of a project unused for cache.retention should go once its last session is collected")
	}
	if _, err := os.Stat(filepath.Join(f.root, "sessions", "github.com")); err == nil {
		t.Error("the emptied parent folders of the key should go too")
	}
}

// inRepoSession writes a session an earlier sdd logged into checkout's
// .sdd/sessions, ended at ended unless that is zero, and stages one attachment
// of it in the project's state folder, where sdd stages for every location.
func (f stateFixture) inRepoSession(t *testing.T, key, checkout string, id sdd.SessionID, ended time.Time) {
	t.Helper()
	sessions, err := localadapter.NewFilesystemSessionStoreAt(filepath.Join(checkout, ".sdd", "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	metadata := sdd.SessionMetadata{ID: id, Subject: "local", Project: "local", Participant: "Christopher", UpdatedAt: f.now.Add(-200 * 24 * time.Hour)}
	if _, err := sessions.Create(t.Context(), metadata); err != nil {
		t.Fatal(err)
	}
	blobs, err := localadapter.NewFilesystemStagedBlobStore(f.location(key))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := blobs.Stage(t.Context(), sdd.SessionRef{Subject: "local", Session: id}, "evidence.md", strings.NewReader("evidence")); err != nil {
		t.Fatal(err)
	}
	if !ended.IsZero() {
		f.endInRepoSession(t, checkout, id, ended)
	}
}

func (f stateFixture) endInRepoSession(t *testing.T, checkout string, id sdd.SessionID, ended time.Time) {
	t.Helper()
	sessions, err := localadapter.NewFilesystemSessionStoreAt(filepath.Join(checkout, ".sdd", "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	stored, err := sessions.Load(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	metadata := stored.Metadata
	metadata.Ended = &sdd.SessionEnd{Act: sdd.SessionConcluded, EndedAt: ended}
	payload, err := json.Marshal(metadata.Ended)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Append(t.Context(), id, stored.Version, sdd.SessionAppend{Metadata: &metadata, Events: []sdd.StoredEvent{{CodecVersion: sdd.SessionCodecVersion, Code: sdd.SessionEndedEventCode, Payload: payload}}}); err != nil {
		t.Fatal(err)
	}
}

// Retiring an unused project keeps the staged attachments of a session still
// open in the .sdd/sessions of any checkout sdd opened the project from, or
// whose log there it cannot read, and of every session while a recorded
// checkout cannot be read or no checkout is on record, since an open session
// may lie there; an ended session's go.
func TestStateSweepKeepsTheStagedAttachmentsOfOpenInRepoSessions(t *testing.T) {
	long := 3 * sweepUnusedRetention
	for _, tc := range []struct {
		name       string
		unrecorded bool
		prepare    func(t *testing.T, f stateFixture, key, checkout string)
		wantKept   bool
	}{
		{name: "open", wantKept: true, prepare: func(t *testing.T, f stateFixture, key, checkout string) {
			f.inRepoSession(t, key, checkout, "s_inrepo", time.Time{})
		}},
		{name: "unreadable", wantKept: true, prepare: func(t *testing.T, f stateFixture, key, checkout string) {
			f.inRepoSession(t, key, checkout, "s_inrepo", time.Time{})
			if err := os.WriteFile(filepath.Join(checkout, ".sdd", "sessions", "s_inrepo.jsonl"), []byte("{not json\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "open in a second checkout", wantKept: true, prepare: func(t *testing.T, f stateFixture, key, checkout string) {
			f.inRepoSession(t, key, checkout, "s_first", f.now.Add(-time.Hour))
			second := t.TempDir()
			if err := localadapter.RecordCheckout(f.root, key, second, f.now); err != nil {
				t.Fatal(err)
			}
			f.inRepoSession(t, key, second, "s_inrepo", time.Time{})
		}},
		{name: "ended", prepare: func(t *testing.T, f stateFixture, key, checkout string) {
			f.inRepoSession(t, key, checkout, "s_inrepo", f.now.Add(-time.Hour))
		}},
		{name: "checkout gone, as an unmounted volume", wantKept: true, prepare: func(t *testing.T, f stateFixture, key, checkout string) {
			f.inRepoSession(t, key, checkout, "s_inrepo", time.Time{})
			if err := os.RemoveAll(checkout); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "no checkout on record", unrecorded: true, wantKept: true, prepare: func(t *testing.T, f stateFixture, key, checkout string) {
			f.inRepoSession(t, key, checkout, "s_inrepo", time.Time{})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStateFixture(t)
			const key = "github.com/acme/paused"
			checkout := t.TempDir()
			if !tc.unrecorded {
				if err := localadapter.RecordCheckout(f.root, key, checkout, f.now); err != nil {
					t.Fatal(err)
				}
			}
			f.used(t, key, f.now.Add(-long))
			tc.prepare(t, f, key, checkout)

			f.sweep(t)

			if kept := f.stagedExists(t, key, "s_inrepo"); kept != tc.wantKept {
				t.Errorf("the staged attachments of the in-repo session kept should be %v", tc.wantKept)
			}
			if tc.unrecorded {
				return
			}
			record := filepath.Join(f.root, "checkouts", "github.com", "acme", "paused.list")
			if _, err := os.Stat(record); (err == nil) != tc.wantKept {
				t.Errorf("the checkout record should stay exactly while staged attachments do, stat says %v", err)
			}
		})
	}
}

// A retired project whose staged attachments an open in-repo session kept is
// swept again, through its checkout record, and loses them once that session
// ends.
func TestStateSweepRemovesTheKeptStagedAttachmentsOnceTheInRepoSessionEnds(t *testing.T) {
	f := newStateFixture(t)
	const key = "github.com/acme/paused"
	checkout := t.TempDir()
	if err := localadapter.RecordCheckout(f.root, key, checkout, f.now); err != nil {
		t.Fatal(err)
	}
	f.used(t, key, f.now.Add(-3*sweepUnusedRetention))
	f.inRepoSession(t, key, checkout, "s_inrepo", time.Time{})

	f.sweep(t)
	if !f.stagedExists(t, key, "s_inrepo") {
		t.Fatal("an open in-repo session should keep its staged attachments")
	}
	f.endInRepoSession(t, checkout, "s_inrepo", f.now)
	later := f
	later.now = f.now.Add(time.Hour)
	later.sweep(t)

	if f.stagedExists(t, key, "s_inrepo") {
		t.Error("the staged attachments should go once the in-repo session ended")
	}
}

func TestRecordCheckoutRecordsEachCheckoutOnce(t *testing.T) {
	f := newStateFixture(t)
	const key = "local/abcdef012345"
	for _, checkout := range []string{"/work/app", "/work/app-feature", "/work/app"} {
		if err := localadapter.RecordCheckout(f.root, key, checkout, f.now); err != nil {
			t.Fatal(err)
		}
	}
	content, err := os.ReadFile(filepath.Join(f.root, "checkouts", "local", "abcdef012345.list"))
	if err != nil {
		t.Fatal(err)
	}
	stamp := f.now.UTC().Format(time.RFC3339)
	if got, want := string(content), "/work/app\t"+stamp+"\n/work/app-feature\t"+stamp+"\n"; got != want {
		t.Errorf("the record should list each checkout once with when it was last seen, got %q, want %q", got, want)
	}
}

// A checkout a sweep has found missing for longer than cache.retention, as a
// removed worktree, is dropped from the record; the attachments only it could
// own then go. A checkout last seen long ago but found missing only now, as a
// volume unmounted after a while without sweeps, counts as missing from now.
func TestStateSweepDropsACheckoutMissingLongerThanTheRetention(t *testing.T) {
	f := newStateFixture(t)
	const key = "github.com/acme/paused"
	checkout := t.TempDir()
	if err := localadapter.RecordCheckout(f.root, key, checkout, f.now.Add(-3*sweepUnusedRetention)); err != nil {
		t.Fatal(err)
	}
	f.used(t, key, f.now.Add(-3*sweepUnusedRetention))
	f.inRepoSession(t, key, checkout, "s_inrepo", time.Time{})
	if err := os.RemoveAll(checkout); err != nil {
		t.Fatal(err)
	}

	f.sweep(t)
	if !f.stagedExists(t, key, "s_inrepo") {
		t.Fatal("a checkout last seen long ago but first found missing now should keep the attachments it may own")
	}
	later := f
	later.now = f.now.Add(sweepUnusedRetention + time.Hour)
	later.sweep(t)

	if f.stagedExists(t, key, "s_inrepo") {
		t.Error("the attachments only a checkout gone past cache.retention could own should go")
	}
	if _, err := os.Stat(filepath.Join(f.root, "checkouts", "github.com", "acme", "paused.list")); err == nil {
		t.Error("the record of a checkout gone past cache.retention should go")
	}
}

// The sweep refreshes when it last saw a recorded checkout, so a checkout
// unmounted later counts as missing from then on, not from its recording.
func TestStateSweepRefreshesWhenItLastSawACheckout(t *testing.T) {
	f := newStateFixture(t)
	const key = "github.com/acme/app"
	checkout := t.TempDir()
	if err := localadapter.RecordCheckout(f.root, key, checkout, f.now.Add(-3*sweepUnusedRetention)); err != nil {
		t.Fatal(err)
	}
	f.used(t, key, f.now)

	f.sweep(t)

	content, err := os.ReadFile(filepath.Join(f.root, "checkouts", "github.com", "acme", "app.list"))
	if err != nil {
		t.Fatal(err)
	}
	if want := checkout + "\t" + f.now.UTC().Format(time.RFC3339) + "\n"; string(content) != want {
		t.Errorf("the record should say the sweep saw the checkout, got %q, want %q", content, want)
	}
}

// A checkout recorded under the repository-identity key and the
// identity-less one has its in-repo sessions collected with the attachments
// they staged under either key.
func TestStateSweepCollectsAnInRepoSessionUnderEveryKeyRecordingItsCheckout(t *testing.T) {
	f := newStateFixture(t)
	const identified, identityless = "github.com/acme/app", "local/abcdef012345"
	checkout := t.TempDir()
	for _, key := range []string{identified, identityless} {
		if err := localadapter.RecordCheckout(f.root, key, checkout, f.now); err != nil {
			t.Fatal(err)
		}
		f.used(t, key, f.now)
	}
	f.inRepoSession(t, identityless, checkout, "s_old", f.now.Add(-sweepSessionRetention-time.Hour))

	removed := f.sweep(t)

	if removed.Sessions != 1 {
		t.Errorf("the sweep should collect the in-repo session once, got %+v", removed)
	}
	if f.stagedExists(t, identityless, "s_old") {
		t.Error("the attachments the session staged under the identity-less key should go with it")
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

// A session written through the store locations sdd resolves stamps its
// project as used, an in-tree session the primary location's project.
func TestSessionWritesRefreshTheProjectsUseStamp(t *testing.T) {
	stateRoot, checkout := t.TempDir(), t.TempDir()
	sddDir := filepath.Join(checkout, ".sdd")
	locations, err := localadapter.SessionLocations(stateRoot, sddDir, "github.com/acme/app", checkout)
	if err != nil {
		t.Fatal(err)
	}
	stamp := filepath.Join(stateRoot, "used", "github.com", "acme", "app.stamp")
	for _, location := range []localadapter.StoreLocation{locations[0], locations[len(locations)-1]} {
		if location.UseStamp != stamp {
			t.Errorf("%s should stamp the project's use, stamps %q", location.Name, location.UseStamp)
		}
	}
	sessions, err := localadapter.NewFilesystemSessionStore(locations...)
	if err != nil {
		t.Fatal(err)
	}
	metadata := sdd.SessionMetadata{ID: "s_new", Subject: "local", Project: "github.com/acme/app", Participant: "Christopher", UpdatedAt: time.Now().UTC()}
	created, err := sessions.Create(t.Context(), metadata)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stamp); err != nil {
		t.Fatalf("creating a session should stamp the project: %v", err)
	}
	stale := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(stamp, stale, stale); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Append(t.Context(), "s_new", created.Version, sdd.SessionAppend{Events: []sdd.StoredEvent{{CodecVersion: sdd.SessionCodecVersion, Code: "test.event"}}}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(stamp); err != nil || !info.ModTime().After(stale.Add(time.Hour)) {
		t.Errorf("appending to a session should refresh a day-old stamp, got %v, %v", info.ModTime(), err)
	}
}

// The sweep collects the ended sessions an earlier sdd logged into the
// .sdd/sessions of a recorded checkout, with the attachments they staged in
// the state folder; open and recently ended ones stay.
func TestStateSweepCollectsTheEndedSessionsOfRecordedCheckouts(t *testing.T) {
	f := newStateFixture(t)
	const key = "github.com/acme/app"
	checkout := t.TempDir()
	if err := localadapter.RecordCheckout(f.root, key, checkout, f.now); err != nil {
		t.Fatal(err)
	}
	f.used(t, key, f.now)
	f.inRepoSession(t, key, checkout, "s_old", f.now.Add(-sweepSessionRetention-time.Hour))
	f.inRepoSession(t, key, checkout, "s_recent", f.now.Add(-time.Hour))
	f.inRepoSession(t, key, checkout, "s_open", time.Time{})

	removed := f.sweep(t)

	if removed.Sessions != 1 {
		t.Errorf("the sweep should collect the one in-repo session ended past sessions.retention, got %+v", removed)
	}
	for id, wantKept := range map[sdd.SessionID]bool{"s_old": false, "s_recent": true, "s_open": true} {
		_, err := os.Stat(filepath.Join(checkout, ".sdd", "sessions", string(id)+".jsonl"))
		if kept := err == nil; kept != wantKept {
			t.Errorf("in-repo session %s kept should be %v", id, wantKept)
		}
		if kept := f.stagedExists(t, key, id); kept != wantKept {
			t.Errorf("the staged attachments of in-repo session %s kept should be %v", id, wantKept)
		}
	}
}

// A checkout record line without its last-seen time is malformed: the sweep
// fails on it and keeps the attachments the record would judge.
func TestStateSweepRefusesACheckoutRecordLineWithoutATime(t *testing.T) {
	f := newStateFixture(t)
	const key = "github.com/acme/paused"
	checkout := t.TempDir()
	record := filepath.Join(f.root, "checkouts", "github.com", "acme", "paused.list")
	if err := os.MkdirAll(filepath.Dir(record), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(record, []byte(checkout+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.used(t, key, f.now.Add(-3*sweepUnusedRetention))
	f.inRepoSession(t, key, checkout, "s_inrepo", f.now.Add(-time.Hour))

	_, err := localadapter.StateSweep{StateRoot: f.root}.Sweep(t.Context(), f.now, sweepSessionRetention, sweepUnusedRetention)

	if err == nil || !strings.Contains(err.Error(), "checkout record") {
		t.Errorf("the sweep should fail on the malformed record, got %v", err)
	}
	if !f.stagedExists(t, key, "s_inrepo") {
		t.Error("attachments a malformed record would judge should stay")
	}
}

// A command opening a project replaces a checkout record it cannot read, so a
// corrupt record never keeps the project from opening; the sweep still fails
// on one (TestStateSweepRefusesACheckoutRecordLineWithoutATime).
func TestRecordCheckoutReplacesAMalformedRecord(t *testing.T) {
	f := newStateFixture(t)
	const key = "local/abcdef012345"
	record := filepath.Join(f.root, "checkouts", "local", "abcdef012345.list")
	if err := os.MkdirAll(filepath.Dir(record), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(record, []byte("/work/old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := localadapter.RecordCheckout(f.root, key, "/work/app", f.now); err != nil {
		t.Fatalf("recording a checkout over a malformed record should succeed: %v", err)
	}
	content, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if want := "/work/app\t" + f.now.UTC().Format(time.RFC3339) + "\n"; string(content) != want {
		t.Errorf("the malformed record should be replaced, got %q, want %q", content, want)
	}
}

// A checkout found missing and then back, as a volume mounted again, counts
// as missing only from when it next goes.
func TestStateSweepForgetsAMissingCheckoutThatCameBack(t *testing.T) {
	f := newStateFixture(t)
	const key = "github.com/acme/paused"
	checkout := filepath.Join(t.TempDir(), "volume")
	away := checkout + "-unmounted"
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := localadapter.RecordCheckout(f.root, key, checkout, f.now); err != nil {
		t.Fatal(err)
	}
	f.used(t, key, f.now.Add(-3*sweepUnusedRetention))
	f.inRepoSession(t, key, checkout, "s_inrepo", time.Time{})
	sweepAt := func(at time.Time) {
		t.Helper()
		later := f
		later.now = at
		later.sweep(t)
	}

	if err := os.Rename(checkout, away); err != nil {
		t.Fatal(err)
	}
	sweepAt(f.now)
	if err := os.Rename(away, checkout); err != nil {
		t.Fatal(err)
	}
	sweepAt(f.now.Add(time.Hour))
	if err := os.Rename(checkout, away); err != nil {
		t.Fatal(err)
	}
	sweepAt(f.now.Add(sweepUnusedRetention + 2*time.Hour))

	if !f.stagedExists(t, key, "s_inrepo") {
		t.Error("a checkout missing again for a moment should keep the attachments it may own")
	}
}
