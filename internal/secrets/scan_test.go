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
	"github.com/networkteam/sdd/internal/testsupport"
)

func ruleIDs(findings []secrets.Finding) []string {
	ids := make([]string, 0, len(findings))
	for _, finding := range findings {
		ids = append(ids, finding.RuleID)
	}
	return ids
}

func TestScanFixtures(t *testing.T) {
	fixtures := testsupport.SecretFixtures()
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
	content, err := fs.ReadFile(testsupport.SecretFixtures(), "clean.txt")
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
	content, err := fs.ReadFile(testsupport.SecretFixtures(), "positive/github-pat.txt")
	if err != nil {
		t.Fatal(err)
	}

	token := strings.TrimSpace(string(content))

	findings, err := secrets.Scan("first line\nsecond line\n" + token + "\nfourth line\n" + token + "\n")

	if err != nil {
		t.Fatal(err)
	}
	var lines []int
	for _, finding := range findings {
		if finding.RuleID == "github-pat" {
			lines = append(lines, finding.Line)
		}
	}
	if !slices.Equal(lines, []int{3, 5}) {
		t.Errorf("each finding should point at the line holding its token: %+v", findings)
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
	content, err := fs.ReadFile(testsupport.SecretFixtures(), "positive/github-pat.txt")
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
	content, err := fs.ReadFile(testsupport.SecretFixtures(), "positive/gitlab-pat.txt")
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(string(content))
	cases := []struct {
		name  string
		entry model.Entry
		where string
		line  string
	}{
		{name: "body", entry: model.Entry{Content: "Setup:\n" + token}, where: "the body", line: "line 2)"},
		{name: "summary", entry: model.Entry{Summary: token}, where: "the summary", line: "line 1)"},
		{name: "frontmatter field", entry: model.Entry{Type: model.TypeSignal, Kind: model.KindGap, Aliases: []string{"first", token}}, where: "the frontmatter field aliases", line: "line 3)"},
		{name: "block scalar with a blank line", entry: model.Entry{Type: model.TypeSignal, Kind: model.KindGap, Refs: []model.Ref{{ID: "20260101-120000-s-tac-aaa", Kind: "addresses", Desc: "first\n\n" + token}}}, where: "the frontmatter field refs", line: "line 7)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := secrets.CheckEntry(&tc.entry)

			if !errors.Is(err, secrets.ErrCredential) {
				t.Fatalf("the entry should be refused as holding a credential: %v", err)
			}
			if !strings.Contains(err.Error(), tc.where+" looks like") || !strings.Contains(err.Error(), "gitlab-pat") {
				t.Errorf("the refusal should name the part and the rule: %v", err)
			}
			if !strings.Contains(err.Error(), tc.line) {
				t.Errorf("the refusal should give the line within the part: %v", err)
			}
			if strings.Contains(err.Error(), token) {
				t.Error("the refusal must never repeat the credential")
			}
		})
	}
}

func TestRefusedPassageLocatesTheCredential(t *testing.T) {
	content, err := fs.ReadFile(testsupport.SecretFixtures(), "positive/github-pat.txt")
	if err != nil {
		t.Fatal(err)
	}
	sample := strings.TrimSpace(string(content))
	findings, err := secrets.Scan(sample)
	if err != nil || len(findings) == 0 {
		t.Fatalf("the sample should hold a credential: %v", err)
	}
	column := findings[0].Column
	for _, finding := range findings {
		column = min(column, finding.Column)
	}
	match := sample[column:]

	t.Run("words before it", func(t *testing.T) {
		passage := secrets.RefusedPassage(secrets.CheckText("body", "intro\nThe old notes held "+sample+" for weeks."))

		if !strings.HasPrefix(passage, "The old notes held") {
			t.Errorf("the passage should be the words before the credential on its line, got %q", passage)
		}
		held, err := secrets.Scan(passage)
		if err != nil || len(held) != 0 {
			t.Errorf("the passage must not hold the credential: %v", ruleIDs(held))
		}
	})
	t.Run("nothing before it", func(t *testing.T) {
		passage := secrets.RefusedPassage(secrets.CheckText("body", "Setup:\n"+match))

		if passage != "body" {
			t.Errorf("a credential opening its line should be located by the part name, got %q", passage)
		}
	})
	if passage := secrets.RefusedPassage(secrets.CheckText("body", "A clean body.")); passage != "" {
		t.Errorf("clean text should have no refused passage, got %q", passage)
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
	fixtures := testsupport.SecretFixtures()
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

func TestGenericAPIKeyNeedsADigitOrMixedCase(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		finding bool
	}{
		{name: "prose heading", text: "# Key questions: ownership/delegation\n", finding: false},
		{name: "value with a digit", text: "# Key insight: zqvmeplurk/x7hdnwa\n", finding: true},
		{name: "value with mixed case", text: "# Key questions: Ownership/delegation\n", finding: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings, err := secrets.Scan(tc.text)
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(ruleIDs(findings), "generic-api-key") != tc.finding {
				t.Errorf("generic-api-key should judge the value by digits and letter case, raised %v", ruleIDs(findings))
			}
		})
	}
}
