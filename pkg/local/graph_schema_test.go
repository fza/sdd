package local_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

func TestFilesystemGraphStoreReportsTheGraphSchema(t *testing.T) {
	cases := []struct {
		name string
		meta string
		path bool
		want int
	}{
		{name: "recorded schema", meta: "{\"graph_schema_version\": 1}\n", path: true, want: 1},
		{name: "no meta path", path: false, want: model.CurrentGraphSchemaVersion},
		{name: "missing meta file", path: true, want: model.CurrentGraphSchemaVersion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			metaPath := filepath.Join(root, ".sdd", model.SchemaMetaFileName)
			if tc.meta != "" {
				err := os.MkdirAll(filepath.Dir(metaPath), 0o755)
				if err != nil {
					t.Fatal(err)
				}
				err = os.WriteFile(metaPath, []byte(tc.meta), 0o644)
				if err != nil {
					t.Fatal(err)
				}
			}
			options := localadapter.FilesystemGraphStoreOptions{Project: "example", GraphDir: filepath.Join(root, ".sdd", "graph")}
			if tc.path {
				options.MetaPath = metaPath
			}
			store, err := localadapter.NewFilesystemGraphStore(options)
			if err != nil {
				t.Fatal(err)
			}
			version, err := store.GraphSchemaVersion(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if version != tc.want {
				t.Errorf("the store should report the schema meta.json records, got %d", version)
			}
			snapshot, err := store.Current(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.SchemaVersion() != tc.want {
				t.Errorf("a snapshot should carry the graph's schema, got %d", snapshot.SchemaVersion())
			}
		})
	}
}

func TestFilesystemGraphStoreRefusesAnUnreadableMeta(t *testing.T) {
	root := t.TempDir()
	metaPath := filepath.Join(root, model.SchemaMetaFileName)
	err := os.WriteFile(metaPath, []byte("not json"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	store, err := localadapter.NewFilesystemGraphStore(localadapter.FilesystemGraphStoreOptions{Project: "example", GraphDir: filepath.Join(root, "graph"), MetaPath: metaPath})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.GraphSchemaVersion(t.Context())
	if err == nil || !strings.Contains(err.Error(), metaPath) {
		t.Errorf("a broken meta.json should fail naming the file, got %v", err)
	}
}
