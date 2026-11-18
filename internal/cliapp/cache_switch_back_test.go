package cliapp_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/index"
	sdd "github.com/networkteam/sdd/pkg/application"
	localadapter "github.com/networkteam/sdd/pkg/local"
	"github.com/networkteam/sdd/test"
)

// otherSddState writes the machine state of two projects as an sdd that
// keeps no use stamp, checkout record or relevance ledger leaves it, all of
// it last touched at old, and returns the folders holding it: two index
// stores, one written to an hour ago, each with a replaced and a current
// version of one entry; an ended and an open session with their staged
// attachments, and the staged attachment of a session whose log is gone; an
// ended session under an identity-less project key.
func otherSddState(t *testing.T, cacheRoot, stateRoot string, old time.Time) []string {
	t.Helper()
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	short := func(name string) string {
		sum := sha256.Sum256([]byte(name))
		return hex.EncodeToString(sum[:4])
	}
	at := old.UTC().Format(time.RFC3339)

	const entry = "20260102-030405-s-tac-abc"
	replaced, current := entry+"#v-aaaaaaaaaaaaaaaa#summary", entry+"#v-bbbbbbbbbbbbbbbb#summary"
	manifest := `{"version":1,"entries":{"` + entry + `":{"versions":[` +
		`{"hash":"aaaaaaaaaaaaaaaa","fingerprint":"test/embedder@2","derivation":"v1","chunk_ids":["` + replaced + `"],"indexed_at":"` + at + `"},` +
		`{"hash":"bbbbbbbbbbbbbbbb","fingerprint":"test/embedder@2","derivation":"v1","chunk_ids":["` + current + `"],"indexed_at":"` + old.Add(time.Hour).UTC().Format(time.RFC3339) + `"}]}}}`
	stores := filepath.Join(cacheRoot, "index", "example.test", "other")
	unused, inUse := filepath.Join(stores, "0123456789ab"), filepath.Join(stores, "ba9876543210")
	for _, store := range []string{unused, inUse} {
		rows := filepath.Join(store, "chromem", short(index.CollectionName))
		write(filepath.Join(store, ".lock"), "")
		write(filepath.Join(store, "manifest.json"), manifest)
		write(filepath.Join(store, "generation"), "2")
		write(filepath.Join(rows, "00000000.gob"), "collection")
		write(filepath.Join(rows, short(replaced)+".gob"), "replaced row")
		write(filepath.Join(rows, short(current)+".gob"), "current row")
	}

	created := func(id, project string) string {
		return `{"version":1,"metadata":{"ID":"` + id + `","Subject":"local","Project":"` + project + `","Participant":"Other Author","Label":"","Attachment":null,"UpdatedAt":"` + at + `"}}` + "\n"
	}
	ended := func(id, project string) string {
		end := `{"Act":"concluded","EndedAt":"` + at + `"}`
		return created(id, project) +
			`{"version":2,"metadata":{"ID":"` + id + `","Subject":"local","Project":"` + project + `","Participant":"Other Author","Label":"","Attachment":null,"Ended":` + end + `,"UpdatedAt":"` + at + `"},` +
			`"events":[{"Sequence":1,"CreatedAt":"` + at + `","CodecVersion":1,"Code":"session_ended","Payload":` + end + `}]}` + "\n"
	}
	logs := map[string][2]string{
		"example.test/other": {ended("s_ended", "example.test/other"), created("s_open", "example.test/other")},
		"local/0123456789ab": {ended("s_ended", "local"), ""},
	}
	folders := []string{stores}
	for key, log := range logs {
		sessions := filepath.Join(stateRoot, "sessions", filepath.FromSlash(key))
		staged := filepath.Join(stateRoot, "staged-blobs", filepath.FromSlash(key), "local")
		for i, id := range []string{"s_ended", "s_open"} {
			if log[i] == "" {
				continue
			}
			write(filepath.Join(sessions, id+".jsonl"), log[i])
			write(filepath.Join(sessions, id+".jsonl.lock"), "")
			write(filepath.Join(staged, id, "0123456789abcdef0123456789abcdef.blob"), "evidence")
		}
		write(filepath.Join(staged, "s_gone", "fedcba9876543210fedcba9876543210.blob"), "evidence")
		folders = append(folders, sessions, filepath.Dir(staged))
	}

	for _, folder := range folders {
		err := filepath.WalkDir(folder, func(path string, _ fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			return os.Chtimes(path, old, old)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	recent := time.Now().Add(-time.Hour)
	for _, name := range []string{"manifest.json", "generation"} {
		if err := os.Chtimes(filepath.Join(inUse, name), recent, recent); err != nil {
			t.Fatal(err)
		}
	}
	return folders
}

// The cache and state folders are shared with every sdd on the machine. After
// this sdd worked in its own project and ran its whole clean-up, a switch
// back to an sdd that keeps no use records must find the files of its other
// projects as it left them, to the byte: index stores, session logs and
// staged attachments.
func TestCachePruneLeavesTheStateOfProjectsItNeverUsedByteForByte(t *testing.T) {
	f := newAppFixture(t)
	cacheRoot, stateRoot := filepath.Join(f.root, "cache", "sdd"), filepath.Join(f.root, "state", "sdd")
	now := time.Now().UTC().Round(0)
	folders := otherSddState(t, cacheRoot, stateRoot, now.Add(-400*24*time.Hour))
	before := map[string]map[string]string{}
	for _, folder := range folders {
		before[folder] = test.TreeSnapshot(t, folder)
	}

	own := localadapter.StoreLocation{
		Name:        "test",
		Sessions:    filepath.Join(stateRoot, "sessions", "example.test", "cli"),
		StagedBlobs: filepath.Join(stateRoot, "staged-blobs", "example.test", "cli"),
		Subject:     "local",
		Project:     "example.test/cli",
	}
	sessions, err := localadapter.NewFilesystemSessionStore(own)
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := localadapter.NewFilesystemStagedBlobStore(own)
	if err != nil {
		t.Fatal(err)
	}
	endedAt := now.Add(-30 * 24 * time.Hour)
	metadata := sdd.SessionMetadata{ID: "s_ended", Subject: "local", Project: "example.test/cli", Participant: "Local Author", UpdatedAt: endedAt}
	created, err := sessions.Create(t.Context(), metadata)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := blobs.Stage(t.Context(), sdd.SessionRef{Subject: "local", Session: "s_ended"}, "evidence.md", strings.NewReader("evidence")); err != nil {
		t.Fatal(err)
	}
	metadata.Ended = &sdd.SessionEnd{Act: sdd.SessionConcluded, EndedAt: endedAt}
	payload, err := json.Marshal(metadata.Ended)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Append(t.Context(), "s_ended", created.Version, sdd.SessionAppend{Metadata: &metadata, Events: []sdd.StoredEvent{{CodecVersion: sdd.SessionCodecVersion, Code: sdd.SessionEndedEventCode, Payload: payload}}}); err != nil {
		t.Fatal(err)
	}

	ownStore := index.StoreDir(cacheRoot, "example.test/cli", "test/embedder@2")
	expiredRow, keptRow := []string{"e#v-old#summary"}, []string{"e#v-new#summary"}
	err = index.WriteStore(t.Context(), ownStore, func(idx *index.Index) error {
		m := &index.Manifest{Version: 1, Entries: map[string]index.EntryState{}}
		for _, v := range []index.EntryVersion{
			{Hash: "old", Derivation: index.DerivationCurrent, ChunkIDs: expiredRow, IndexedAt: endedAt.Add(-time.Hour)},
			{Hash: "new", Derivation: index.DerivationCurrent, ChunkIDs: keptRow, IndexedAt: endedAt},
		} {
			row := index.Row{EntryID: "e", EntryHash: v.Hash, ChunkID: v.ChunkIDs[0], Text: "t", Body: "t", ContentHash: index.HashContent("t"), ModelFingerprint: "m", IsSummary: true, Embedding: []float32{1, 0}}
			if err := idx.UpsertEntry(context.Background(), "e", nil, []index.Row{row}); err != nil {
				return err
			}
			m.AddVersion("e", v)
		}
		return m.Save(ownStore)
	})
	if err != nil {
		t.Fatal(err)
	}

	f.serve(t, func(*mcp.ClientSession) {})
	record, err := os.ReadFile(filepath.Join(stateRoot, "checkouts", "example.test", "cli.list"))
	if err != nil {
		t.Fatalf("serving the project should record its checkout: %v", err)
	}
	checkout, seen, _ := strings.Cut(strings.TrimSuffix(string(record), "\n"), "\t")
	if _, err := time.Parse(time.RFC3339, seen); err != nil || checkout == "" || !strings.HasSuffix(seen, "Z") {
		t.Fatalf("the checkout record should hold the path and when it was seen, in UTC, got %q", record)
	}

	r := f.run(t, "cache", "prune")
	if r.err != nil {
		t.Fatalf("cache prune: %v\n%s", r.err, r.stderr)
	}

	if !strings.Contains(r.stdout, "1 index versions") || !strings.Contains(r.stdout, "1 sessions") {
		t.Errorf("the run should clean this sdd's own project, stdout:\n%s", r.stdout)
	}
	if _, err := os.Stat(filepath.Join(own.Sessions, "s_ended.jsonl")); !os.IsNotExist(err) {
		t.Errorf("this sdd's own ended session should be collected, stat says %v", err)
	}
	if index.DocumentsSize(ownStore, expiredRow) != 0 || index.DocumentsSize(ownStore, keptRow) == 0 {
		t.Error("this sdd's own store should lose only its replaced version")
	}
	for _, folder := range folders {
		if changes := test.TreeChanges(before[folder], test.TreeSnapshot(t, folder)); len(changes) > 0 {
			t.Errorf("%s should stay byte for byte, the run left: %v", folder, changes)
		}
	}
}
