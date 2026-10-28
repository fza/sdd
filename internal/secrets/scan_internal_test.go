package secrets

import "testing"

// Clean text must not pay for the rule set: a rule's pattern compiles only
// once one of its keywords occurs.
func TestScanCompilesNoPatternForTextWithoutKeywords(t *testing.T) {
	set, err := loadRules()
	if err != nil {
		t.Fatal(err)
	}
	var keywordless []string
	for _, r := range set.Rules {
		if len(r.Keywords) == 0 && r.Regex != "" {
			keywordless = append(keywordless, r.ID)
		}
	}

	_, err = Scan("zzzz qqqq")

	if err != nil {
		t.Fatal(err)
	}
	for _, r := range set.Rules {
		if len(r.Keywords) > 0 && r.regex != nil {
			t.Errorf("rule %s compiled without any of its keywords in the text", r.ID)
		}
	}
	if len(keywordless) > 0 {
		t.Logf("rules without keywords always run: %v", keywordless)
	}
}

func TestEveryVendoredRuleCompiles(t *testing.T) {
	set, err := loadRules()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range set.Rules {
		err = r.compilePatterns()
		if err != nil {
			t.Error(err)
		}
	}
}
