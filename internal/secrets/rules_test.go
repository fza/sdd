package secrets_test

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/internal/testsupport"
)

// gitleaks.toml in the package folder is the file the package embeds.
func TestTheBuiltinRuleFileReadsStrictly(t *testing.T) {
	data, err := os.ReadFile("gitleaks.toml")
	if err != nil {
		t.Fatal(err)
	}
	stampData, err := os.ReadFile("gitleaks.source.json")
	if err != nil {
		t.Fatal(err)
	}
	var stamp struct {
		Tag string `json:"tag"`
	}
	err = json.Unmarshal(stampData, &stamp)
	if err != nil {
		t.Fatal(err)
	}

	set, err := secrets.ParseRules(data)

	if err != nil {
		t.Fatalf("the built-in file should decode strictly and compile whole: %v", err)
	}
	if stamp.Tag == "" || len(set.RuleIDs()) == 0 {
		t.Errorf("the built-in file should carry its release tag and its rules, got tag %q", stamp.Tag)
	}
}

func TestParseRulesRefusesAnInvalidFile(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
	}{
		{name: "unknown key", fixture: "unknown-key.toml"},
		{name: "rule regex that does not compile", fixture: "bad-rule-regex.toml"},
		{name: "rule path that does not compile", fixture: "bad-rule-path.toml"},
		{name: "allowlist regex that does not compile", fixture: "bad-allowlist-regex.toml"},
		{name: "allowlist path that does not compile", fixture: "bad-allowlist-path.toml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := secrets.ParseRules(testsupport.SecretRuleFixture(t, tc.fixture))

			if err == nil {
				t.Error("the file should be refused")
			}
		})
	}
}

func TestParseFetchedRulesNeedsEveryBuiltinRule(t *testing.T) {
	_, err := secrets.ParseFetchedRules(testsupport.SecretRuleFixture(t, "one-rule.toml"))
	if err == nil || !strings.Contains(err.Error(), "github-pat") {
		t.Errorf("a copy lacking built-in rules should be refused naming them, got %v", err)
	}

	set, err := secrets.ParseFetchedRules(testsupport.NewerGitleaksRules(t))
	if err != nil {
		t.Fatalf("a copy keeping every built-in rule should pass: %v", err)
	}
	if !slices.Contains(set.RuleIDs(), "sdd-fixture-token") {
		t.Error("the copy's added rule should be read")
	}
}

func TestParseRulesNormalizesAsGitleaks(t *testing.T) {
	set, err := secrets.ParseRules(testsupport.SecretRuleFixture(t, "normalized.toml"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		text    string
		finding bool
	}{
		{name: "uppercase keyword matches lowercase text", text: testsupport.FixtureToken, finding: true},
		{name: "lowercase and condition needs every check", text: "example " + testsupport.FixtureToken, finding: true},
		{name: "uppercase stopword matches", text: "example sddfixture_allowedq2m5n8p4r", finding: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings, err := set.Scan(tc.text)
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(ruleIDs(findings), "sdd-fixture-token") != tc.finding {
				t.Errorf("the rule should judge the text as gitleaks does, raised %v", ruleIDs(findings))
			}
		})
	}
}

func TestParseRulesReadsBothAllowlistForms(t *testing.T) {
	set, err := secrets.ParseRules(testsupport.SecretRuleFixture(t, "allowlist-forms.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"global-single", "global-multiple", "rule-single", "rule-multiple"} {
		t.Run(marker, func(t *testing.T) {
			findings, err := set.Scan(marker + " " + testsupport.FixtureToken)
			if err != nil {
				t.Fatal(err)
			}
			if len(findings) != 0 {
				t.Errorf("the allowlist should apply, raised %v", ruleIDs(findings))
			}
		})
	}
	findings, err := set.Scan("unlisted " + testsupport.FixtureToken)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) == 0 {
		t.Error("text no allowlist names should raise the rule")
	}
}
