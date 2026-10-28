package handlers_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/model"
)

func TestMigrateSchema(t *testing.T) {
	cases := []struct {
		name       string
		graph      int
		wantErr    string
		wantMeta   string
		wantCommit bool
	}{
		{name: "an older graph is stamped and committed", graph: 1, wantMeta: `"graph_schema_version": 2`, wantCommit: true},
		{name: "a current graph is left alone", graph: 2, wantMeta: `"graph_schema_version": 2`},
		{name: "a newer graph is refused", graph: 3, wantErr: "upgrade sdd", wantMeta: `"graph_schema_version": 3`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sddDir := t.TempDir()
			metaPath := filepath.Join(sddDir, "meta.json")
			if err := os.WriteFile(metaPath, []byte(fmt.Sprintf(`{"graph_schema_version": %d}`, tc.graph)), 0o644); err != nil {
				t.Fatal(err)
			}
			committer := &messageRecordingCommitter{}
			h := handlers.New(handlers.Options{GraphDir: t.TempDir(), Reader: &graphReader{}, Committer: committer})

			var stamped bool
			err := h.MigrateSchema(context.Background(), &command.MigrateSchemaCmd{
				SDDDir: sddDir, SchemaVersion: 2,
				OnStamped: func(from, to int) { stamped = true },
			})
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("the migration should be refused naming the way forward, got %v", err)
				}
			} else if err != nil {
				t.Fatalf("MigrateSchema: %v", err)
			}
			data, err := os.ReadFile(metaPath)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), tc.wantMeta) {
				t.Errorf("meta.json should hold the expected schema version:\n%s", data)
			}
			if stamped != tc.wantCommit || (len(committer.messages) == 1) != tc.wantCommit {
				t.Errorf("only a stamp should be reported and committed, got stamped=%v commits=%v", stamped, committer.messages)
			}
		})
	}
}

// changeReader serves a fixed set of modified paths and their HEAD content.
type changeReader struct {
	modified []string
	head     map[string]string
}

func (c changeReader) ModifiedFiles(context.Context, string) ([]string, error) {
	return c.modified, nil
}

func (c changeReader) HeadContent(_ context.Context, path string) ([]byte, error) {
	return []byte(c.head[path]), nil
}

func TestCommitStrandedTitles(t *testing.T) {
	graphDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const untitled = "---\ntype: signal\nlayer: process\nkind: gap\nsummary: S.\n---\n\nBody.\n"
	const titled = "---\ntype: signal\nlayer: process\nkind: gap\ntitle: a title\nsummary: S.\n---\n\nBody.\n"
	files := map[string]struct{ head, working string }{
		"20260101-120000-s-prc-aaa": {head: untitled, working: titled},                                         // stranded
		"20260101-120001-s-prc-bbb": {head: titled, working: strings.Replace(titled, "Body", "Edited", 1)},     // titled before
		"20260101-120002-s-prc-ccc": {head: untitled, working: strings.Replace(untitled, "Body", "Edited", 1)}, // no title gained
	}
	reader := changeReader{head: map[string]string{}}
	for id, f := range files {
		writeEntryFile(t, graphDir, id, f.working)
		rel, err := model.IDToRelPath(id)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(graphDir, rel)
		reader.modified = append(reader.modified, path)
		reader.head[path] = f.head
	}
	attachment := filepath.Join(graphDir, "2026", "01", "01-120000-s-prc-aaa", "notes.md")
	stray := filepath.Join(graphDir, "2026", "01", "notes.md")
	if err := os.WriteFile(stray, []byte("not an entry\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reader.modified = append(reader.modified, attachment, stray)

	committer := &pathRecordingCommitter{}
	h := handlers.New(handlers.Options{GraphDir: graphDir, Reader: &graphReader{}, Committer: committer, Changes: reader})
	var committed int
	if err := h.CommitStrandedTitles(context.Background(), &command.CommitStrandedTitlesCmd{OnCommitted: func(n int) { committed = n }}); err != nil {
		t.Fatalf("CommitStrandedTitles: %v", err)
	}
	strandedRel, _ := model.IDToRelPath("20260101-120000-s-prc-aaa")
	if committed != 1 || len(committer.paths) != 1 || committer.paths[0] != filepath.Join(graphDir, strandedRel) {
		t.Errorf("only the entry that gained a title on disk should be committed, got %d reported, paths %v", committed, committer.paths)
	}
}

type pathRecordingCommitter struct{ paths []string }

func (c *pathRecordingCommitter) Commit(_ string, paths ...string) error {
	c.paths = append(c.paths, paths...)
	return nil
}
