package cliapp_test

import (
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/testsupport"
)

// noProviderConfig points the model runner at a port that accepts no
// connection and configures no embedder, so a request that slips past the
// guard fails instead of reaching a provider.
const noProviderConfig = "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\n" +
	"llm:\n  provider: openai\n  model: gpt-test\n  endpoint: http://127.0.0.1:1\n  api_keys:\n    openai: sk-test-not-a-key\n"

func secretFixture(t *testing.T, rule string) string {
	t.Helper()
	content, err := fs.ReadFile(testsupport.SecretFixtures(), "positive/"+rule+".txt")
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

// A credential-shaped --query phrase is searched literally, never embedded,
// so it works without any embedding provider and says so on stderr.
func TestSearchQueryWithACredentialSearchesLiterally(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", noProviderConfig)
	credential := secretFixture(t, "gitlab-pat")
	f.write(t, "decisions/2026/01/01-120000-s-tac-lek.md",
		"---\ntype: signal\nlayer: tactical\nkind: gap\nconfidence: high\nparticipants: [Local Author]\nsummary: A leaked token.\n---\n\nThe old notes held "+credential+".\n")

	result := f.run(t, "search", "--query", credential, "--limit", "5", "--max-citations", "0")

	if result.err != nil {
		t.Fatalf("a literal search needs no embedding provider: %v\n%s", result.err, result.stderr)
	}
	if !strings.Contains(result.stdout, "20260101-120000-s-tac-lek") {
		t.Errorf("the literal search should find the entry holding the phrase:\n%s", result.stdout)
	}
	if !strings.Contains(result.stderr, "searched for it literally") {
		t.Errorf("the output should say the phrase was searched literally:\n%s", result.stderr)
	}
}
