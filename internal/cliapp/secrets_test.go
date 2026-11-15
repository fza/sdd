package cliapp_test

import (
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/test"
)

// noProviderConfig points the model runner at a port that accepts no
// connection and configures no embedder, so a request that slips past the
// guard fails instead of reaching a provider.
const noProviderConfig = "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\n" +
	"llm:\n  provider: ollama\n  model: llama3\n  ollama_endpoint: http://127.0.0.1:1\n"

func secretFixture(t *testing.T, rule string) string {
	t.Helper()
	content, err := fs.ReadFile(test.SecretFixtures(), "positive/"+rule+".txt")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(content))
}

func graphFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// sdd new refuses a body holding a credential and writes and commits nothing.
func TestNewRefusesACredential(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", noProviderConfig)
	runGit(t, f.root, "init", "-q", "-b", "main")
	runGit(t, f.root, "config", "user.email", "test@example.com")
	runGit(t, f.root, "config", "user.name", "Test")
	runGit(t, f.root, "commit", "-q", "--allow-empty", "-m", "seed")
	graphDir := filepath.Join(f.root, "decisions")
	before := graphFiles(t, graphDir)

	result := f.run(t, "new", "signal", "tactical", "Deployment notes:\n"+secretFixture(t, "aws-access-token"),
		"--confidence", "high", "--summary", "Deployment notes.")

	if result.err == nil || !strings.Contains(result.err.Error(), "the body looks like a credential (aws-access-token") {
		t.Fatalf("the capture should be refused naming the body and the rule: %v\n%s", result.err, result.stderr)
	}
	if after := graphFiles(t, graphDir); !slices.Equal(after, before) {
		t.Errorf("a refused capture should write nothing, the graph holds %v", after)
	}
	if subject := runGit(t, f.root, "log", "-1", "--format=%s"); subject != "seed" {
		t.Errorf("a refused capture should commit nothing, HEAD is %q", subject)
	}
}
