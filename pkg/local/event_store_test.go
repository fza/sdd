package local_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/segment"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/local"
	"github.com/networkteam/sdd/pkg/sddtest"
)

func TestFilesystemEventStoreConformance(t *testing.T) {
	sddtest.RunEventStoreTests(t, func(t *testing.T) sddtest.EventStoreFixture {
		return sddtest.EventStoreFixture{Store: newFileEventStore(t, filepath.Join(t.TempDir(), "events"), "outside")}
	})
}

func TestMemoryEventStoreConformance(t *testing.T) {
	sddtest.RunEventStoreTests(t, func(*testing.T) sddtest.EventStoreFixture {
		return sddtest.EventStoreFixture{Store: local.NewMemoryEventStore()}
	})
}

func TestFilesystemEventStore_WritesOneNewSegmentPerAppendAndChannel(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "events")
	store := newFileEventStore(t, dir)
	err := store.Append(t.Context(), []sdd.Event{testEvent("a", "recheck"), testEvent("b", "recheck"), testEvent("c", "wip")})
	if err != nil {
		t.Fatal(err)
	}
	first := segmentFiles(t, filepath.Join(dir, "recheck"))
	if len(first) != 1 || len(segmentFiles(t, filepath.Join(dir, "wip"))) != 1 {
		t.Fatalf("one append should write one segment per channel, recheck holds %v", first)
	}
	name := regexp.MustCompile(`^\d{8}-\d{6}\.\d{9}-[0-9a-f]{16}\.seg$`)
	if !name.MatchString(first[0]) {
		t.Fatalf("segment name %q is not <time>-<random>.seg", first[0])
	}
	before, err := os.ReadFile(filepath.Join(dir, "recheck", first[0]))
	if err != nil {
		t.Fatal(err)
	}
	err = store.Append(t.Context(), []sdd.Event{testEvent("d", "recheck")})
	if err != nil {
		t.Fatal(err)
	}
	second := segmentFiles(t, filepath.Join(dir, "recheck"))
	if len(second) != 2 || !slices.Contains(second, first[0]) {
		t.Fatalf("a second append should add a segment beside the first, recheck holds %v", second)
	}
	after, err := os.ReadFile(filepath.Join(dir, "recheck", first[0]))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("a later append rewrote an existing segment")
	}
}

func TestFilesystemEventStore_SharingFollowsTheChannel(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "events")
	err := newFileEventStore(t, dir).Append(t.Context(), []sdd.Event{testEvent("a", "recheck"), testEvent("b", "wip")})
	if err != nil {
		t.Fatal(err)
	}
	for _, channel := range []string{"recheck", "wip"} {
		ignore, err := os.ReadFile(filepath.Join(dir, channel, ".gitignore"))
		if err != nil || !strings.Contains(string(ignore), "\n*\n") {
			t.Fatalf("local channel %s should ignore itself, .gitignore = %q, %v", channel, ignore, err)
		}
	}

	err = newFileEventStore(t, dir, "recheck").Append(t.Context(), []sdd.Event{testEvent("c", "recheck")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.Stat(filepath.Join(dir, "recheck", ".gitignore"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a channel turned git should drop the ignore file sdd wrote: %v", err)
	}
	_, err = os.Stat(filepath.Join(dir, "wip", ".gitignore"))
	if err != nil {
		t.Fatalf("an unrelated local channel lost its ignore file: %v", err)
	}

	userIgnore := []byte("*.tmp\n")
	err = os.MkdirAll(filepath.Join(dir, "outside"), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(dir, "outside", ".gitignore"), userIgnore, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	err = newFileEventStore(t, dir, "outside").Append(t.Context(), []sdd.Event{testEvent("d", "outside")})
	if err != nil {
		t.Fatal(err)
	}
	kept, err := os.ReadFile(filepath.Join(dir, "outside", ".gitignore"))
	if err != nil || !bytes.Equal(kept, userIgnore) {
		t.Fatalf("an ignore file sdd did not write should stay, got %q, %v", kept, err)
	}
}

func TestFilesystemEventStore_CorruptSegmentFailsLoud(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "events")
	store := newFileEventStore(t, dir)
	err := store.Append(t.Context(), []sdd.Event{testEvent("a", "recheck")})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "recheck", segmentFiles(t, filepath.Join(dir, "recheck"))[0])
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 0xff
	err = os.WriteFile(path, data, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Query(t.Context(), sdd.EventQuery{})
	if !errors.Is(err, segment.ErrCorrupt) || !strings.Contains(err.Error(), path) {
		t.Fatalf("Query over a corrupt segment = %v; want ErrCorrupt naming the file", err)
	}
}

func TestNewFilesystemEventStore_RefusesInvalidOptions(t *testing.T) {
	_, err := local.NewFilesystemEventStore(local.FilesystemEventStoreOptions{})
	if err == nil {
		t.Error("a store without a directory was accepted")
	}
	_, err = local.NewFilesystemEventStore(local.FilesystemEventStoreOptions{Dir: t.TempDir(), GitChannels: []string{"../graph"}})
	if err == nil {
		t.Error("a git channel that is no channel name was accepted")
	}
}

func TestFilesystemEventStore_RefusesMalformedRecords(t *testing.T) {
	record := func(writer string, entries uint64, trailing ...byte) []byte {
		out := segment.AppendString(nil, "e1")
		out = segment.AppendString(out, "sdd.recheck")
		out = segment.AppendVarint(out, time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC).UnixNano())
		out = segment.AppendString(out, writer)
		out = segment.AppendString(out, "")
		out = segment.AppendUvarint(out, entries)
		if entries > 0 {
			out = segment.AppendString(out, "20261109-004506-d-cpt-udz")
		}
		out = segment.AppendBytes(out, nil)
		return append(out, trailing...)
	}
	cases := map[string][]byte{
		"bytes after the record": record("sdd", 1, 0x00),
		"incomplete envelope":    record("", 1),
		"entries past the end":   record("sdd", 3),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "events")
			err := os.MkdirAll(filepath.Join(dir, "recheck"), 0o755)
			if err != nil {
				t.Fatal(err)
			}
			err = os.WriteFile(filepath.Join(dir, "recheck", "20261109-120000.000000000-0000000000000000.seg"), segment.Encode("sdd.events", [][]byte{data}), 0o644)
			if err != nil {
				t.Fatal(err)
			}
			_, err = newFileEventStore(t, dir).Query(t.Context(), sdd.EventQuery{})
			if !errors.Is(err, segment.ErrCorrupt) {
				t.Fatalf("Query = %v, want ErrCorrupt", err)
			}
		})
	}
	valid := filepath.Join(t.TempDir(), "events")
	err := os.MkdirAll(filepath.Join(valid, "recheck"), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(valid, "recheck", "20261109-120000.000000000-0000000000000000.seg"), segment.Encode("sdd.events", [][]byte{record("sdd", 1)}), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	got, err := newFileEventStore(t, valid).Query(t.Context(), sdd.EventQuery{})
	if err != nil || len(got) != 1 {
		t.Fatalf("the well-formed control record = %v, %v", got, err)
	}
}

func TestFilesystemEventStore_ReadsOnlySegmentsOfValidChannels(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "events")
	store := newFileEventStore(t, dir)
	err := store.Append(t.Context(), []sdd.Event{testEvent("a", "recheck")})
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"recheck/.20261109-000000.000000000-0000000000000000.seg.tmp": "partial",
		"recheck/notes.txt": "notes",
		"Not A Channel/20261109-000000.000000000-0000000000000000.seg": "garbage",
	} {
		filename := filepath.Join(dir, filepath.FromSlash(name))
		err := os.MkdirAll(filepath.Dir(filename), 0o755)
		if err != nil {
			t.Fatal(err)
		}
		err = os.WriteFile(filename, []byte(content), 0o644)
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.Query(t.Context(), sdd.EventQuery{})
	if err != nil {
		t.Fatalf("Query should pass over temporary files and foreign directories: %v", err)
	}
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("Query = %+v", got)
	}
	empty, err := newFileEventStore(t, filepath.Join(t.TempDir(), "absent")).Query(t.Context(), sdd.EventQuery{})
	if err != nil || len(empty) != 0 {
		t.Fatalf("a store whose directory does not exist yet = %v, %v; want no events", empty, err)
	}
}

func TestEventSegments_GitChannelRidesTheNextAutoCommit(t *testing.T) {
	committers := map[string]func(repo *gitRepository) error{
		"entry commit": func(repo *gitRepository) error {
			repo.write("notes/entry.md", "entry\n")
			return git.CLI{Dir: repo.root}.Commit("sdd: capture", filepath.Join(repo.root, "notes/entry.md"))
		},
		"removal commit": func(repo *gitRepository) error {
			repo.write("notes/marker.md", "marker\n")
			repo.git("add", "notes/marker.md")
			repo.git("commit", "-m", "test: add marker")
			err := os.Remove(filepath.Join(repo.root, "notes/marker.md"))
			if err != nil {
				return err
			}
			return git.RemovalCommitter{Dir: repo.root}.Commit("sdd: finish wip", "notes/marker.md")
		},
		"mutation finalizer": func(repo *gitRepository) error {
			repo.write(".sdd/graph/2026/11/09-120000-s-tac-evt.md", "entry\n")
			finalizer := local.GitFinalizer{Checkout: repo.root, GraphDir: ".sdd/graph", Branch: "main"}
			return finalizer.Finalize(t.Context(), sdd.AppliedMutation{BatchID: "mutation-events", Batch: sdd.MutationBatch{
				Message: "sdd: signal tactical captured", Changes: []sdd.DocumentChange{{LogicalPath: "2026/11/09-120000-s-tac-evt.md"}},
			}})
		},
	}
	for name, commit := range committers {
		t.Run(name, func(t *testing.T) {
			repo := newGitRepository(t)
			store := newFileEventStore(t, filepath.Join(repo.root, ".sdd", "events"), "recheck")
			head := repo.git("rev-parse", "HEAD")
			err := store.Append(t.Context(), []sdd.Event{testEvent("a", "recheck"), testEvent("b", "wip")})
			if err != nil {
				t.Fatal(err)
			}
			if repo.git("rev-parse", "HEAD") != head {
				t.Fatal("appending events made a commit of its own")
			}
			err = commit(repo)
			if err != nil {
				t.Fatal(err)
			}
			committed := strings.Fields(repo.git("show", "--name-only", "--format=", "HEAD"))
			recheck := ".sdd/events/recheck/" + segmentFiles(t, filepath.Join(repo.root, ".sdd/events/recheck"))[0]
			if !slices.Contains(committed, recheck) {
				t.Fatalf("the git channel's segment should ride the commit, which holds %v", committed)
			}
			for _, path := range committed {
				if strings.HasPrefix(path, ".sdd/events/wip/") {
					t.Fatalf("the local channel reached the commit: %v", committed)
				}
			}
			if status := repo.git("status", "--porcelain", "--untracked-files=all", "--", ".sdd/events"); status != "" {
				t.Fatalf("event segments left untracked and visible after the commit:\n%s", status)
			}
		})
	}
}

func newFileEventStore(t *testing.T, dir string, gitChannels ...string) *local.FilesystemEventStore {
	t.Helper()
	store, err := local.NewFilesystemEventStore(local.FilesystemEventStoreOptions{Dir: dir, GitChannels: gitChannels})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func testEvent(id, channel string) sdd.Event {
	return sdd.Event{
		ID: id, Channel: channel, Type: "sdd." + channel, Time: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC), Writer: "sdd",
		Subject: sdd.EventSubject{Entries: []string{"20261109-004506-d-cpt-udz"}},
	}
}

func segmentFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if segment.IsName(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	return names
}
