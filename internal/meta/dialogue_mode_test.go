package meta_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/meta"
)

// A personal layer carrying a dialogue.mode sdd does not know stops the load,
// naming the file, the value and the values it takes.
func TestReadConfigLayersRefusesAnUnknownDialogueMode(t *testing.T) {
	cases := []struct {
		name     string
		mode     string
		override bool
	}{
		{name: "former autonomous in config.local.yaml", mode: "autonomous"},
		{name: "unknown value in config.local.yaml", mode: "free"},
		{name: "former autonomous in --local-config", mode: "autonomous", override: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sddDir := t.TempDir()
			path := filepath.Join(sddDir, meta.LocalConfigFileName)
			override := ""
			if tc.override {
				path = filepath.Join(t.TempDir(), "elsewhere.yaml")
				override = path
			}
			if err := os.WriteFile(path, []byte("dialogue:\n  mode: "+tc.mode+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			_, _, err := meta.ReadConfigLayers(sddDir, override)
			if err == nil {
				t.Fatal("an unknown dialogue mode should stop the load")
			}
			for _, part := range []string{path, tc.mode, "guided or auto"} {
				if !strings.Contains(err.Error(), part) {
					t.Errorf("the refusal should name the file, the value and the valid values, missing %q: %v", part, err)
				}
			}
		})
	}
}

// The committed layer may not set dialogue.mode at all; any value there is
// dropped with a warning rather than refused.
func TestReadConfigLayersDropsTheCommittedDialogueMode(t *testing.T) {
	sddDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sddDir, "config.yaml"), []byte("dialogue:\n  mode: autonomous\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	committed, _, err := meta.ReadConfigLayers(sddDir, "")
	if err != nil {
		t.Fatalf("the committed layer's dialogue mode should not stop the load: %v", err)
	}
	if committed.Dialogue.Mode != "" {
		t.Errorf("the committed layer's dialogue mode should be dropped, got %q", committed.Dialogue.Mode)
	}
}

// Locating the project and its graph reads past a refused dialogue.mode in
// the machine-local layer; the config load still refuses it.
func TestLayoutReadsPassAnUnknownDialogueMode(t *testing.T) {
	root := t.TempDir()
	sddDir := meta.SDDDir(root)
	if err := os.MkdirAll(sddDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sddDir, "config.yaml"), []byte("graph_dir: decisions\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sddDir, meta.LocalConfigFileName), []byte("dialogue:\n  mode: autonomous\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	project, err := meta.ResolveProject(root, nil)
	if err != nil {
		t.Fatalf("project resolution should not judge the dialogue mode: %v", err)
	}
	if project.Root != root {
		t.Errorf("the project should resolve to its own root, got %q", project.Root)
	}
	layout, err := meta.ReadLayoutConfig(sddDir, "")
	if err != nil {
		t.Fatalf("the layout read should not judge the dialogue mode: %v", err)
	}
	if layout.GraphDir != "decisions" {
		t.Errorf("the layout read should carry the graph dir, got %q", layout.GraphDir)
	}
	_, err = meta.ReadConfig(sddDir, "")
	if err == nil {
		t.Error("the config load should still refuse the dialogue mode")
	}
}
