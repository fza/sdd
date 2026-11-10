package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLLMDebugWritesATranscriptPerCall(t *testing.T) {
	cases := []struct {
		name        string
		local       string
		committed   string
		transcripts bool
	}{
		{name: "unset", local: "participant: Ada\n"},
		{name: "on in the machine-local config", local: "participant: Ada\nllm:\n  debug: true\n", transcripts: true},
		{name: "on in the committed config only", local: "participant: Ada\n", committed: "llm:\n  debug: true\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			project := newMigrateProject(t)
			writeFile(t, filepath.Join(project, ".sdd", "config.local.yaml"), tc.local)
			if tc.committed != "" {
				committed := readFile(t, filepath.Join(project, ".sdd", "config.yaml"))
				writeFile(t, filepath.Join(project, ".sdd", "config.yaml"), strings.Replace(committed, "llm:\n", tc.committed, 1))
			}

			output, err := runReleased(t, project, "", "new", "s", "tac", "--confidence", "high", "--skip-preflight", "--summary", "S.", "A new gap.")
			if err != nil {
				t.Fatalf("the write should go through: %v\n%s", err, output)
			}
			files, err := os.ReadDir(filepath.Join(project, ".sdd", "tmp", "llm-debug"))
			if !tc.transcripts {
				if err == nil && len(files) > 0 {
					t.Fatalf("no transcript should be written, got %v", files)
				}
				return
			}
			if err != nil || len(files) == 0 {
				t.Fatalf("each call should leave a transcript, got %v (%v)", files, err)
			}
			transcript := readFile(t, filepath.Join(project, ".sdd", "tmp", "llm-debug", files[0].Name()))
			for _, want := range []string{"- provider: ollama", "## User prompt", "## Response", "migrated title"} {
				if !strings.Contains(transcript, want) {
					t.Errorf("the transcript should carry %q:\n%s", want, transcript)
				}
			}
		})
	}
}
