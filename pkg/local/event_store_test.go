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
	first := eventFiles(t, filepath.Join(dir, "recheck"))
	if len(first) != 1 || len(eventFiles(t, filepath.Join(dir, "wip"))) != 1 {
		t.Fatalf("one append should write one segment per channel, recheck holds %v", first)
	}
	name := regexp.MustCompile(`^\d{8}-\d{6}\.\d{9}-[0-9a-f]{16}\.jsonl$`)
	if !name.MatchString(first[0]) {
		t.Fatalf("segment name %q is not <time>-<random>.jsonl", first[0])
	}
	before, err := os.ReadFile(filepath.Join(dir, "recheck", first[0]))
	if err != nil {
		t.Fatal(err)
	}
	err = store.Append(t.Context(), []sdd.Event{testEvent("d", "recheck")})
	if err != nil {
		t.Fatal(err)
	}
	second := eventFiles(t, filepath.Join(dir, "recheck"))
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

// Each line holds one event's envelope, the payload as JSON where it is
// compact JSON and as base64 under payload_base64 otherwise; the channel is
// the directory's.
func TestFilesystemEventStore_WritesOneEventPerLine(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "events")
	jsonPayload, binaryPayload := testEvent("a", "recheck"), testEvent("b", "recheck")
	jsonPayload.Payload = []byte(`{"kind":"strain"}`)
	binaryPayload.Payload = []byte{0x00, 0xff}
	err := newFileEventStore(t, dir).Append(t.Context(), []sdd.Event{jsonPayload, binaryPayload})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "recheck", eventFiles(t, filepath.Join(dir, "recheck"))[0]))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"a","type":"sdd.recheck","time":"2026-11-09T12:00:00Z","writer":"sdd","subject":{"entries":["20261108-120000-d-cpt-f01"]},"payload":{"kind":"strain"}}
{"id":"b","type":"sdd.recheck","time":"2026-11-09T12:00:00Z","writer":"sdd","subject":{"entries":["20261108-120000-d-cpt-f01"]},"payload_base64":"AP8="}
`
	if string(data) != want {
		t.Fatalf("segment holds\n%s\nwant one JSON line per event without the channel:\n%s", data, want)
	}
}

// A JSON Lines segment that does not decode whole fails the read and the
// compaction, naming the file, instead of reading as fewer events.
func TestFilesystemEventStore_CorruptJSONLinesFailLoud(t *testing.T) {
	valid := `{"id":"a","type":"sdd.recheck","time":"2026-11-09T12:00:00Z","writer":"sdd","subject":{"entries":["20261108-120000-d-cpt-f01"]}}` + "\n" +
		`{"id":"b","type":"sdd.recheck","time":"2026-11-09T12:01:00Z","writer":"sdd","subject":{"entries":["20261108-120000-d-cpt-f01"]}}` + "\n"
	cases := map[string]string{
		"torn last line":              strings.TrimSuffix(valid, "\n"),
		"last line cut mid-way":       valid[:len(valid)-40],
		"empty file":                  "",
		"blank line":                  strings.Replace(valid, "\n", "\n\n", 1),
		"line that is no JSON":        valid + "not json\n",
		"torn line a newer sdd wrote": valid + `{"id":"c","type":"sdd.recheck","time":"2026-11-09T12:02:00Z","writer":"sdd","subject":{"entries":["x"]},"extra":1}`,
		"line missing its time":       valid + `{"id":"c","type":"sdd.recheck","writer":"sdd","subject":{"entries":["x"]}}` + "\n",
		"another channel's line":      valid + `{"id":"c","channel":"wip","type":"sdd.recheck","time":"2026-11-09T12:02:00Z","writer":"sdd","subject":{"entries":["x"]}}` + "\n",
		"incomplete envelope":         valid + `{"id":"c","type":"sdd.recheck","time":"2026-11-09T12:02:00Z","subject":{"entries":["x"]}}` + "\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "events")
			path := writeRecheckFile(t, dir, "20261109-120000.000000000-0000000000000000.jsonl", []byte(content))
			store := newFileEventStore(t, dir)
			_, err := store.Query(t.Context(), sdd.EventQuery{})
			if !errors.Is(err, segment.ErrCorrupt) || !strings.Contains(err.Error(), path) {
				t.Fatalf("Query = %v; want ErrCorrupt naming the file", err)
			}
			err = store.Compact(t.Context())
			if !errors.Is(err, segment.ErrCorrupt) {
				t.Fatalf("Compact = %v; want ErrCorrupt", err)
			}
			kept, err := os.ReadFile(path)
			if err != nil || string(kept) != content {
				t.Fatalf("a failed compaction should leave the corrupt segment as it was: %v", err)
			}
		})
	}
	dir := filepath.Join(t.TempDir(), "events")
	writeRecheckFile(t, dir, "20261109-120000.000000000-0000000000000000.jsonl", []byte(valid))
	got, err := newFileEventStore(t, dir).Query(t.Context(), sdd.EventQuery{})
	if err != nil || len(got) != 2 {
		t.Fatalf("the well-formed control segment = %v, %v", got, err)
	}
}

// A line a newer sdd wrote, with fields this one does not know, reads as its
// envelope, and compaction keeps the line as it was.
func TestFilesystemEventStore_ReadsLinesWithFieldsItDoesNotKnow(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "events")
	newer := `{"id":"a","type":"sdd.recheck","time":"2026-11-09T12:00:00Z","writer":"sdd","subject":{"entries":["20261108-120000-d-cpt-f01"],"since":"x"},"payload":{"kind":"strain"},"retention":"7d"}`
	writeRecheckFile(t, dir, "20261109-120000.000000000-0000000000000000.jsonl", []byte(newer+"\n"))
	store := newFileEventStore(t, dir)
	known := testEvent("b", "recheck")
	known.Time = known.Time.Add(time.Minute)
	err := store.Append(t.Context(), []sdd.Event{known})
	if err != nil {
		t.Fatal(err)
	}
	read := testEvent("a", "recheck")
	read.Payload = []byte(`{"kind":"strain"}`)
	got, err := store.Query(t.Context(), sdd.EventQuery{})
	if err != nil {
		t.Fatalf("Query should read past fields it does not know: %v", err)
	}
	assertEvents(t, got, []sdd.Event{read, known})

	err = store.Compact(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	files := eventFiles(t, filepath.Join(dir, "recheck"))
	if len(files) != 1 {
		t.Fatalf("compaction should merge the two segments, the channel holds %v", files)
	}
	data, err := os.ReadFile(filepath.Join(dir, "recheck", files[0]))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), newer+"\n") {
		t.Fatalf("compaction should keep the newer sdd's line byte for byte, the merged segment holds:\n%s", data)
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

func TestFilesystemEventStore_ReadsOnlySegmentsOfValidChannels(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "events")
	store := newFileEventStore(t, dir)
	err := store.Append(t.Context(), []sdd.Event{testEvent("a", "recheck")})
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"recheck/.20261109-000000.000000000-0000000000000000.jsonl.tmp": "partial",
		"recheck/.hidden.jsonl": "partial",
		"recheck/notes.txt":     "notes",
		"Not A Channel/20261109-000000.000000000-0000000000000000.jsonl": "garbage",
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
			return git.CLI{}.Commit("sdd: capture", "notes/entry.md")
		},
		"removal commit": func(repo *gitRepository) error {
			repo.write("notes/marker.md", "marker\n")
			repo.git("add", "notes/marker.md")
			repo.git("commit", "-m", "test: add marker")
			err := os.Remove(filepath.Join(repo.root, "notes/marker.md"))
			if err != nil {
				return err
			}
			return git.RemovalCommitter{}.Commit("sdd: finish wip", "notes/marker.md")
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
			// The auto-commit paths run in the working directory, as the CLI does.
			t.Chdir(repo.root)
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
			recheck := ".sdd/events/recheck/" + eventFiles(t, filepath.Join(repo.root, ".sdd/events/recheck"))[0]
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
		Subject: sdd.EventSubject{Entries: []string{"20261108-120000-d-cpt-f01"}},
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

// eventFiles lists the JSON Lines segments of a channel directory.
func eventFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, ".") && strings.HasSuffix(name, ".jsonl") {
			names = append(names, name)
		}
	}
	return names
}

// writeRecheckFile writes data as the file name of the recheck channel under
// dir and returns its path.
func writeRecheckFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	err := os.MkdirAll(filepath.Join(dir, "recheck"), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "recheck", name)
	err = os.WriteFile(path, data, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func assertEvents(t *testing.T, got, want []sdd.Event) {
	t.Helper()
	want = slices.Clone(want)
	slices.SortFunc(want, func(a, b sdd.Event) int {
		byTime := a.Time.Compare(b.Time)
		if byTime != 0 {
			return byTime
		}
		return strings.Compare(a.ID, b.ID)
	})
	if len(got) != len(want) {
		t.Fatalf("got %d events: %+v", len(got), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.ID != w.ID || g.Channel != w.Channel || g.Type != w.Type || !g.Time.Equal(w.Time) || g.Writer != w.Writer ||
			g.Subject.URI != w.Subject.URI || !slices.Equal(g.Subject.Entries, w.Subject.Entries) || !bytes.Equal(g.Payload, w.Payload) {
			t.Fatalf("event %d = %+v; should read back as appended: %+v", i, g, w)
		}
	}
}
