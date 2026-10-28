package cliapp_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/test"
)

// sdd new refuses a body holding a credential and writes and commits nothing.
func TestNewRefusesACredential(t *testing.T) {
	project := canonicalTemp(t)
	initRepo(t, project)
	writeFile(t, filepath.Join(project, ".sdd", "config.yaml"), "graph_dir: .sdd/graph\n")
	writeFile(t, filepath.Join(project, ".sdd", "config.local.yaml"), "participant: Ada\n")
	err := os.MkdirAll(filepath.Join(project, ".sdd", "graph"), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	content, err := fs.ReadFile(test.SecretFixtures(), "positive/aws-access-token.txt")
	if err != nil {
		t.Fatal(err)
	}

	output, err := runAppErr(t, project,
		"new", "signal", "tactical", "Deployment notes:\n"+strings.TrimSpace(string(content)),
		"--confidence", "high", "--summary", "Deployment notes.", "--title", "Deployment notes",
	)

	if err == nil || !strings.Contains(err.Error(), "the body looks like a credential (aws-access-token") {
		t.Fatalf("the capture should be refused naming the body and the rule: %v\n%s", err, output)
	}
	if entries := graphEntries(t, filepath.Join(project, ".sdd", "graph")); len(entries) != 0 {
		t.Errorf("a refused capture should write nothing, wrote %v", entries)
	}
	if subject := headSubject(t, project); subject != "seed" {
		t.Errorf("a refused capture should commit nothing, HEAD is %q", subject)
	}
}

// A credential-shaped --query phrase is searched literally, never embedded,
// so it works without any embedding provider and says so on stderr.
func TestSearchQueryWithACredentialSearchesLiterally(t *testing.T) {
	project := canonicalTemp(t)
	initRepo(t, project)
	writeFile(t, filepath.Join(project, ".sdd", "config.yaml"), "graph_dir: .sdd/graph\n")
	content, err := fs.ReadFile(test.SecretFixtures(), "positive/gitlab-pat.txt")
	if err != nil {
		t.Fatal(err)
	}
	credential := strings.TrimSpace(string(content))
	writeFile(t, filepath.Join(project, ".sdd", "graph", "2026", "01", "01-120000-s-tac-lek.md"),
		"---\ntype: signal\nlayer: tactical\nkind: gap\nsummary: A leaked token.\n---\n\nThe old notes held "+credential+".\n")

	output, err := runAppErr(t, project, "search", "--query", credential, "--limit", "5", "--max-citations", "0")

	if err != nil {
		t.Fatalf("a literal search needs no embedding provider: %v\n%s", err, output)
	}
	if !strings.Contains(output, "20260101-120000-s-tac-lek") {
		t.Errorf("the literal search should find the entry holding the phrase:\n%s", output)
	}
	if !strings.Contains(output, "searched for it literally") {
		t.Errorf("the output should say the phrase was searched literally:\n%s", output)
	}
}
