package cliapp_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/test"
)

// sdd info names the rule file every scan of the run uses: a fetched copy
// while secrets.rules_update is daily, the built-in file while it is off.
func TestInfoNamesTheSecretRulesInUse(t *testing.T) {
	builtin, err := secrets.BuiltinRuleFile()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		setting string
		source  string
		tag     string
		update  string
	}{
		{name: "default", source: "fetched", tag: "v99.0.0", update: "daily"},
		{name: "off", setting: "secrets:\n  rules_update: \"off\"\n", source: "built-in", tag: builtin.Tag, update: "off"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAppFixture(t)
			f.write(t, "config/sdd/config.yaml", "participant: Global Author\n"+tc.setting)
			releases := test.NewGitleaksReleases(t)
			releases.Publish("v99.0.0", test.NewerGitleaksRules(t))
			dir := filepath.Join(f.root, "cache", "sdd", secrets.CacheDirName)
			err := secrets.NewUpdater(dir, secrets.NewGitHub(releases.URL, releases.URL, releases.Client), nil).Update(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			info := runInfoJSON(t, f)
			text := f.run(t, "info")

			rules, _ := info["secret_rules"].(map[string]any)
			if rules["source"] != tc.source || rules["tag"] != tc.tag || rules["update"] != tc.update {
				t.Errorf("info should name the rule file the setting selects, got %v", rules)
			}
			if !strings.Contains(text.stdout, "Secret rules: gitleaks "+tc.tag+" ("+tc.source+"), updates "+tc.update) {
				t.Errorf("the text form should carry the same line:\n%s", text.stdout)
			}
		})
	}
}

// A user-global secrets.rules_update sdd does not know fails the command
// rather than running with the default.
func TestAnUnknownSecretRulesUpdateFailsTheCommand(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, "config/sdd/config.yaml", "participant: Global Author\nsecrets:\n  rules_update: weekly\n")

	result := f.run(t, "info")

	if result.err == nil {
		t.Fatalf("the command should fail, got stdout %q", result.stdout)
	}
	for _, part := range []string{filepath.Join("config", "sdd", "config.yaml"), "secrets.rules_update", "weekly", "daily or off"} {
		if !strings.Contains(result.err.Error(), part) {
			t.Errorf("the error should name the file, the key, the value and the valid values, missing %q: %v", part, result.err)
		}
	}
}
