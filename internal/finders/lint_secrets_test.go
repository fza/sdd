package finders_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/test"
)

func fixtureCredential(t *testing.T, rule string) string {
	t.Helper()
	content, err := fs.ReadFile(test.SecretFixtures(), "positive/"+rule+".txt")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(content))
}

// A credential already in the graph is reported with what to rotate, as an
// advisory: the entry cannot be edited, so a false positive would otherwise
// fail lint for good.
func TestLintReportsCredentialsAlreadyInTheGraph(t *testing.T) {
	credential := fixtureCredential(t, "github-pat")
	inBody, err := model.ParseEntry("20260101-120000-s-prc-aaa.md", "---\ntype: signal\nlayer: process\nkind: gap\nsummary: A gap.\n---\n\nSetup:\n"+credential+"\n")
	if err != nil {
		t.Fatal(err)
	}
	inAttachment := lintEntry(t, "20260101-120001-s-prc-bbb", "Another gap.")
	clean := lintEntry(t, "20260101-120002-s-prc-ccc", "A clean gap.")
	graphDir := t.TempDir()
	attachment := "2026/01/01-120001-s-prc-bbb/env.txt"
	err = os.MkdirAll(filepath.Dir(filepath.Join(graphDir, attachment)), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(graphDir, attachment), []byte(fixtureCredential(t, "slack-bot-token")+"\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	inAttachment.Attachments = []string{attachment}
	g := model.NewGraph([]*model.Entry{inBody, inAttachment, clean})
	g.SetGraphDir(graphDir)

	res, err := finders.New(finders.Options{Config: &model.PerRepoConfig{}}).OnGraph(g).Lint(query.LintQuery{})

	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		entry string
		where string
	}{
		{entry: inBody.ID, where: "the body"},
		{entry: inAttachment.ID, where: "the attachment env.txt"},
	}
	for _, tc := range cases {
		var credentialFindings []query.LintFinding
		for _, finding := range findingsFor(res, tc.entry) {
			if finding.Category == "secrets" {
				credentialFindings = append(credentialFindings, finding)
			}
		}
		if len(credentialFindings) != 1 || !strings.Contains(credentialFindings[0].Message, tc.where) {
			t.Errorf("%s should carry one credential finding naming where it sits, got %+v", tc.entry, credentialFindings)
			continue
		}
		if credentialFindings[0].Severity != query.LintAdvisory {
			t.Errorf("a credential already in the graph should be an advisory, got %s", credentialFindings[0].Severity)
		}
		if strings.Contains(credentialFindings[0].Message, credential) {
			t.Error("the finding must never repeat the credential")
		}
	}
	if got := findingsFor(res, clean.ID); len(got) != 0 {
		t.Errorf("a clean entry should raise nothing, got %+v", got)
	}
	if res.Errors() != 0 {
		t.Error("credential advisories should not count as errors")
	}
}
