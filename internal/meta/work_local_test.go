package meta_test

import (
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

// A run starting in a repository that points at its graph through
// project_dir resolves builtin < global < project < the project's local <
// the work repository's local.
func TestReadConfigLayersLaysTheWorkLocalOverTheProjectLocal(t *testing.T) {
	cases := []struct {
		name        string
		work        map[string]string
		wantModel   string
		wantTimeout string
	}{
		{
			name:        "work local over the project's local",
			work:        map[string]string{"config.yaml": "project_dir: ../project\n", "config.local.yaml": "llm:\n  model: work\n"},
			wantModel:   "work",
			wantTimeout: "5m",
		},
		{
			name:        "project_dir set in the work local itself",
			work:        map[string]string{"config.yaml": "skill_scope: project\n", "config.local.yaml": "project_dir: ../project\nllm:\n  model: work\n"},
			wantModel:   "work",
			wantTimeout: "5m",
		},
		{
			name:        "a work repository pointing elsewhere replaces it",
			work:        map[string]string{"config.yaml": "project_dir: ../other\n", "config.local.yaml": "llm:\n  model: work\n"},
			wantModel:   "work",
			wantTimeout: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			work, project := filepath.Join(root, "work"), filepath.Join(root, "project")
			for name, content := range tc.work {
				writeConfig(t, work, name, content)
			}
			writeConfig(t, project, "config.yaml", "graph_dir: .sdd/graph\nllm:\n  provider: openai\n")
			writeConfig(t, project, "config.local.yaml", "llm:\n  model: project\n  timeout: 5m\n")

			cfg, err := meta.ResolveConfig(model.BaseConfig{}, meta.SDDDir(project), meta.LocalConfigPath(meta.SDDDir(work), ""))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.LLM.Model != tc.wantModel {
				t.Errorf("llm.model should come from the work repository's local file, got %q", cfg.LLM.Model)
			}
			if cfg.LLM.Timeout != tc.wantTimeout {
				t.Errorf("llm.timeout should come from the project's local file only beneath its own work repository, got %q", cfg.LLM.Timeout)
			}
			if cfg.LLM.Provider != "openai" {
				t.Errorf("the committed layer should stay beneath both local files, got %q", cfg.LLM.Provider)
			}
		})
	}
}

func TestProjectLocalConfigPathIsTheWorkLocalOfAPointedProject(t *testing.T) {
	root := t.TempDir()
	work, project := filepath.Join(root, "work"), filepath.Join(root, "project")
	writeConfig(t, work, "config.yaml", "project_dir: ../project\n")
	writeConfig(t, project, "config.yaml", "graph_dir: .sdd/graph\n")

	resolved, err := meta.ResolveProject(work, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := resolved.LocalConfigPath(""); got != meta.LocalConfigPath(meta.SDDDir(work), "") {
		t.Errorf("--local should read and write the work repository's local file, got %q", got)
	}
	if got := resolved.LocalConfigPath("/elsewhere.yaml"); got != "/elsewhere.yaml" {
		t.Errorf("--local-config should replace it, got %q", got)
	}
}

// The work repository's local file is machine-local too: it may only raise
// the verify mode the shared layers set.
func TestResolveConfigRefusesAWorkLocalLoweringVerify(t *testing.T) {
	root := t.TempDir()
	work, project := filepath.Join(root, "work"), filepath.Join(root, "project")
	writeConfig(t, work, "config.yaml", "project_dir: ../project\n")
	writeConfig(t, work, "config.local.yaml", "verify:\n  mode: prose\n")
	writeConfig(t, project, "config.yaml", "graph_dir: .sdd/graph\nverify:\n  mode: deep\n")

	_, err := meta.ResolveConfig(model.BaseConfig{}, meta.SDDDir(project), meta.LocalConfigPath(meta.SDDDir(work), ""))
	if err == nil {
		t.Fatal("a work local lowering verify.mode should be refused")
	}
}
