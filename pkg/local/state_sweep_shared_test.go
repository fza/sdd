package local_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	localadapter "github.com/networkteam/sdd/pkg/local"
	"github.com/networkteam/sdd/test"
)

// ageTree sets the modification time of dir and everything below it.
func ageTree(t *testing.T, dir string, at time.Time) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(path, at, at)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The session and staged-attachment folders are shared with every sdd on the
// machine. A project holding no use stamp, checkout record or relevance
// ledger is one this sdd has no record of using: the sweep removes no
// session, staged attachment or leftover folder of it, whatever its age.
func TestStateSweepSkipsAProjectWithoutARecordOfUse(t *testing.T) {
	f := newStateFixture(t)
	long := f.now.Add(-3 * sweepUnusedRetention)
	const (
		sessions = "github.com/acme/other"
		emptied  = "github.com/acme/emptied"
		local    = "local/0123456789ab"
	)
	f.session(t, sessions, "s_ended", long)
	f.session(t, sessions, "s_open", time.Time{})
	f.session(t, local, "s_ended", long)
	leftover := filepath.Join(f.location(emptied).Sessions, "s_gone.lock")
	if err := os.MkdirAll(filepath.Dir(leftover), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(leftover, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	roots := []string{filepath.Join(f.root, "sessions"), filepath.Join(f.root, "staged-blobs")}
	var before []map[string]string
	for _, root := range roots {
		ageTree(t, root, long)
		before = append(before, test.TreeSnapshot(t, root))
	}

	removed := f.sweep(t)

	for i, root := range roots {
		if changes := test.TreeChanges(before[i], test.TreeSnapshot(t, root)); len(changes) > 0 {
			t.Errorf("the state of projects without a record of use should stay byte for byte, the sweep left under %s: %v", filepath.Base(root), changes)
		}
	}
	if removed.Sessions != 0 || removed.Projects != 0 || removed.Unreadable != 0 {
		t.Errorf("the sweep should count nothing, got %+v", removed)
	}
}

// Each record this sdd keeps of a project makes it one the sweep tends.
func TestStateSweepTendsAProjectByAnyRecordOfUse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		record func(t *testing.T, f stateFixture, key string)
	}{
		{name: "a use stamp", record: func(t *testing.T, f stateFixture, key string) { f.used(t, key, f.now) }},
		{name: "a checkout record", record: func(t *testing.T, f stateFixture, key string) {
			if err := localadapter.RecordCheckout(f.root, key, t.TempDir(), f.now); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "a relevance ledger", record: func(t *testing.T, f stateFixture, key string) { f.relevance(t, key) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStateFixture(t)
			const key = "github.com/acme/app"
			f.session(t, key, "s_ended", f.now.Add(-sweepSessionRetention-time.Hour))
			tc.record(t, f, key)

			removed := f.sweep(t)

			if removed.Sessions != 1 || f.logExists(t, key, "s_ended") {
				t.Errorf("the ended session of a project on record should be collected, got %+v", removed)
			}
		})
	}
}
