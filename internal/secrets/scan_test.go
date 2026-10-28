package secrets_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/test"
)

func ruleIDs(findings []secrets.Finding) []string {
	ids := make([]string, 0, len(findings))
	for _, finding := range findings {
		ids = append(ids, finding.RuleID)
	}
	return ids
}

func TestScanFixtures(t *testing.T) {
	fixtures := test.SecretFixtures()
	for _, kind := range []string{"positive", "negative"} {
		entries, err := fs.ReadDir(fixtures, kind)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			rule := strings.TrimSuffix(entry.Name(), path.Ext(entry.Name()))
			t.Run(kind+"/"+rule, func(t *testing.T) {
				content, err := fs.ReadFile(fixtures, path.Join(kind, entry.Name()))
				if err != nil {
					t.Fatal(err)
				}

				findings, err := secrets.Scan(string(content))

				if err != nil {
					t.Fatal(err)
				}
				raised := slices.Contains(ruleIDs(findings), rule)
				if kind == "positive" && !raised {
					t.Errorf("the sample should raise its rule, raised %v", ruleIDs(findings))
				}
				if kind == "negative" && raised {
					t.Error("the allowlisted or low-entropy sample should not raise its rule")
				}
			})
		}
	}
}

func TestScanCleanTextFindsNothing(t *testing.T) {
	content, err := fs.ReadFile(test.SecretFixtures(), "clean.txt")
	if err != nil {
		t.Fatal(err)
	}

	findings, err := secrets.Scan(string(content))

	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Errorf("prose without credentials should raise nothing, raised %v", ruleIDs(findings))
	}
}

func TestScanReportsTheLineOfTheMatch(t *testing.T) {
	content, err := fs.ReadFile(test.SecretFixtures(), "positive/github-pat.txt")
	if err != nil {
		t.Fatal(err)
	}

	findings, err := secrets.Scan("first line\nsecond line\n" + string(content))

	if err != nil {
		t.Fatal(err)
	}
	if len(findings) == 0 || findings[0].Line != 3 {
		t.Errorf("the finding should point at the line holding the token: %+v", findings)
	}
}

func TestScanFileAppliesPathRules(t *testing.T) {
	cases := []struct {
		name   string
		file   string
		raises bool
	}{
		{name: "certificate bundle by name", file: "client.p12", raises: true},
		{name: "same content under another name", file: "client.txt", raises: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings, err := secrets.ScanFile(tc.file, []byte("binary"))
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(ruleIDs(findings), "pkcs12-file") != tc.raises {
				t.Errorf("the path-scoped rule should fire only on its file names, raised %v", ruleIDs(findings))
			}
		})
	}
}

func TestScanFileSkipsGloballyAllowedPaths(t *testing.T) {
	content, err := fs.ReadFile(test.SecretFixtures(), "positive/github-pat.txt")
	if err != nil {
		t.Fatal(err)
	}

	findings, err := secrets.ScanFile("go.sum", content)

	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Errorf("a file the global allowlist names should be skipped, raised %v", ruleIDs(findings))
	}
}

func TestCheckEntryNamesThePart(t *testing.T) {
	content, err := fs.ReadFile(test.SecretFixtures(), "positive/gitlab-pat.txt")
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(string(content))
	cases := []struct {
		name  string
		entry model.Entry
		where string
	}{
		{name: "body", entry: model.Entry{Content: "Setup:\n" + token}, where: "the body"},
		{name: "summary", entry: model.Entry{Summary: token}, where: "the summary"},
		{name: "title", entry: model.Entry{Title: token}, where: "the title"},
		{name: "frontmatter", entry: model.Entry{Aliases: []string{token}}, where: "the frontmatter"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := secrets.CheckEntry(&tc.entry)

			if !errors.Is(err, secrets.ErrCredential) {
				t.Fatalf("the entry should be refused as holding a credential: %v", err)
			}
			if !strings.Contains(err.Error(), tc.where) || !strings.Contains(err.Error(), "gitlab-pat") {
				t.Errorf("the refusal should name the part and the rule: %v", err)
			}
			if strings.Contains(err.Error(), token) {
				t.Error("the refusal must never repeat the credential")
			}
		})
	}
}

type gitleaksSample struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

type gitleaksSamples struct {
	Rule           string           `json:"rule"`
	TruePositives  []gitleaksSample `json:"truePositives"`
	FalsePositives []gitleaksSample `json:"falsePositives"`
}

func (s gitleaksSample) scan() ([]secrets.Finding, error) {
	if s.Path != "" {
		return secrets.ScanFile(s.Path, []byte(s.Text))
	}
	return secrets.Scan(s.Text)
}

// gitleaks' own samples hold the engine to gitleaks' verdicts rule by rule.
func TestScanGitleaksSamples(t *testing.T) {
	fixtures := test.SecretFixtures()
	files, err := fs.ReadDir(fixtures, "gitleaks")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		data, err := fs.ReadFile(fixtures, path.Join("gitleaks", file.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var samples gitleaksSamples
		err = json.Unmarshal(data, &samples)
		if err != nil {
			t.Fatalf("%s: %v", file.Name(), err)
		}
		t.Run(samples.Rule, func(t *testing.T) {
			for i, sample := range samples.TruePositives {
				findings, err := sample.scan()
				if err != nil {
					t.Fatal(err)
				}
				if !slices.Contains(ruleIDs(findings), samples.Rule) {
					t.Errorf("true positive %d should raise the rule, raised %v", i, ruleIDs(findings))
				}
			}
			for i, sample := range samples.FalsePositives {
				findings, err := sample.scan()
				if err != nil {
					t.Fatal(err)
				}
				if slices.Contains(ruleIDs(findings), samples.Rule) {
					t.Errorf("false positive %d should not raise the rule", i)
				}
			}
		})
	}
}
