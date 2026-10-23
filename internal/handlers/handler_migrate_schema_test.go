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
