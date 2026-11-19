package meta_test

import (
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

// A local file from another repository stands in for the project's own; the
// two are never laid over each other.
func TestResolveConfigReadsOneLocalFile(t *testing.T) {
	cases := []struct {
		name string
		work map[string]string
	}{
		{
			name: "a code repository naming the project's graph repository",
			work: map[string]string{"config.yaml": "graph_repo: example.test/acme/app-sdd\n", "config.local.yaml": "llm:\n  model: work\n"},
		},
		{
			name: "a repository naming the project as project_dir",
			work: map[string]string{"config.yaml": "project_dir: ../project\n", "config.local.yaml": "llm:\n  model: work\n"},
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
			if cfg.LLM.Model != "work" {
				t.Errorf("llm.model should come from the local file read, got %q", cfg.LLM.Model)
			}
			if cfg.LLM.Timeout != "" {
				t.Errorf("the project's own local file should not be read beneath it, got llm.timeout %q", cfg.LLM.Timeout)
			}
			if cfg.LLM.Provider != "openai" {
				t.Errorf("the committed layer should stay beneath the local file, got %q", cfg.LLM.Provider)
			}
		})
	}
}

// The work repository's local file is machine-local too: it may only raise
// the verify mode the shared layers set, so a read ignores a lower one.
func TestResolveConfigIgnoresAWorkLocalLoweringVerify(t *testing.T) {
	root := t.TempDir()
	work, project := filepath.Join(root, "work"), filepath.Join(root, "project")
	writeConfig(t, work, "config.yaml", "graph_repo: example.test/acme/app-sdd\n")
	writeConfig(t, work, "config.local.yaml", "verify:\n  mode: prose\n")
	writeConfig(t, project, "config.yaml", "graph_dir: .sdd/graph\nverify:\n  mode: deep\n")

	cfg, err := meta.ResolveConfig(model.BaseConfig{}, meta.SDDDir(project), meta.LocalConfigPath(meta.SDDDir(work), ""))
	if err != nil {
		t.Fatalf("a work local lowering verify.mode should not fail the read: %v", err)
	}
	if got := cfg.Verify.ResolvedMode(); got != model.VerifyDeep {
		t.Fatalf("the shared mode should apply over the ignored work local one, got %q", got)
	}
}
