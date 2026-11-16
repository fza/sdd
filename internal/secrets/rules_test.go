package secrets_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/test"
)

func TestTheBuiltinRuleFileReadsStrictly(t *testing.T) {
	builtin, err := secrets.BuiltinRuleFile()
	if err != nil {
		t.Fatal(err)
	}

	set, err := secrets.ParseFetchedRules(builtin.Data)

	if err != nil {
		t.Fatalf("the built-in file should decode strictly and compile whole: %v", err)
	}
	if builtin.Tag == "" || len(set.RuleIDs()) == 0 {
		t.Errorf("the built-in file should carry its release tag and its rules, got tag %q", builtin.Tag)
	}
}

func TestParseRulesRefusesAnInvalidFile(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		reason  string
	}{
		{name: "unknown key", fixture: "unknown-key.toml"},
		{name: "rule regex that does not compile", fixture: "bad-rule-regex.toml"},
		{name: "rule path that does not compile", fixture: "bad-rule-path.toml"},
		{name: "allowlist regex that does not compile", fixture: "bad-allowlist-regex.toml"},
		{name: "allowlist path that does not compile", fixture: "bad-allowlist-path.toml"},
		{name: "unknown allowlist condition", fixture: "unknown-condition.toml", reason: "XOR"},
		{name: "unknown allowlist regex target", fixture: "unknown-regex-target.toml", reason: "nonsense"},
		{name: "minimum gitleaks version above the one sdd reads", fixture: "newer-min-version.toml", reason: "v99.0.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := secrets.ParseRules(test.SecretRuleFixture(t, tc.fixture))

			if err == nil {
				t.Fatal("the file should be refused")
			}
			if !strings.Contains(err.Error(), tc.reason) {
				t.Errorf("the refusal should name %q, got %v", tc.reason, err)
			}
		})
	}
}

// silencingGitleaksRules keeps every built-in rule id but allowlists every
// secret globally.
func silencingGitleaksRules(t *testing.T) []byte {
	t.Helper()
	return append(test.NewerGitleaksRules(t), "\n[[allowlists]]\nregexes = ['''.''']\n"...)
}

// costlyGitleaksRules adds rules without keywords, which every scan runs on
// all text.
func costlyGitleaksRules(t *testing.T) []byte {
	t.Helper()
	data := test.NewerGitleaksRules(t)
	for i := range 5000 {
		data = fmt.Appendf(data, "\n[[rules]]\nid = \"sdd-costly-%d\"\nregex = '''(?i)[a-z0-9]{8,200}q[a-z0-9 ]{4,300}zx'''\n", i)
	}
	return data
}

func TestParseFetchedRulesNeedsTheBuiltinFindings(t *testing.T) {
	_, err := secrets.ParseFetchedRules(silencingGitleaksRules(t))

	if err == nil || !strings.Contains(err.Error(), "no longer raise their samples: 1password-secret-key") {
		t.Errorf("a copy under which built-in rules raise nothing should be refused naming them, got %v", err)
	}
}

func TestParseFetchedRulesBoundsTheScanCost(t *testing.T) {
	_, err := secrets.ParseFetchedRules(costlyGitleaksRules(t))

	if err == nil || !strings.Contains(err.Error(), "times the built-in") {
		t.Errorf("a copy that makes every scan far costlier should be refused, got %v", err)
	}
}

func TestParseFetchedRulesNeedsEveryBuiltinRule(t *testing.T) {
	_, err := secrets.ParseFetchedRules(test.SecretRuleFixture(t, "one-rule.toml"))
	if err == nil || !strings.Contains(err.Error(), "github-pat") {
		t.Errorf("a copy lacking built-in rules should be refused naming them, got %v", err)
	}

	set, err := secrets.ParseFetchedRules(test.NewerGitleaksRules(t))
	if err != nil {
		t.Fatalf("a copy keeping every built-in rule should pass: %v", err)
	}
	if !slices.Contains(set.RuleIDs(), "sdd-fixture-token") {
		t.Error("the copy's added rule should be read")
	}
}

func TestParseRulesNormalizesAsGitleaks(t *testing.T) {
	set, err := secrets.ParseRules(test.SecretRuleFixture(t, "normalized.toml"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		text    string
		finding bool
	}{
		{name: "uppercase keyword matches lowercase text", text: test.FixtureToken, finding: true},
		{name: "lowercase and condition needs every check", text: "example " + test.FixtureToken, finding: true},
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
	set, err := secrets.ParseRules(test.SecretRuleFixture(t, "allowlist-forms.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"global-single", "global-multiple", "rule-single", "rule-multiple"} {
		t.Run(marker, func(t *testing.T) {
			findings, err := set.Scan(marker + " " + test.FixtureToken)
			if err != nil {
				t.Fatal(err)
			}
			if len(findings) != 0 {
				t.Errorf("the allowlist should apply, raised %v", ruleIDs(findings))
			}
		})
	}
	findings, err := set.Scan("unlisted " + test.FixtureToken)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) == 0 {
		t.Error("text no allowlist names should raise the rule")
	}
}
