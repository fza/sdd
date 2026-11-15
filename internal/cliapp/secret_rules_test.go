package cliapp_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/internal/testsupport"
)

// sdd info and a served info name the rule file every scan of the process
// uses: a fetched copy while secrets.rules_update is daily, the built-in file
// while it is off.
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
			releases := testsupport.NewGitleaksReleases(t)
			releases.Publish("v99.0.0", testsupport.NewerGitleaksRules(t))
			dir := filepath.Join(f.root, "cache", "sdd", secrets.CacheDirName)
			err := secrets.NewUpdater(dir, secrets.NewGitHub(releases.URL, releases.URL, releases.Client), nil).Update(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			text := f.run(t, "info")

			if text.err != nil {
				t.Fatalf("info: %v\n%s", text.err, text.stderr)
			}
			if !strings.Contains(text.stdout, "Secret rules: gitleaks "+tc.tag+" ("+tc.source+"), updates "+tc.update) {
				t.Errorf("info should name the rule file the setting selects:\n%s", text.stdout)
			}
			f.serve(t, func(client *mcp.ClientSession) {
				door, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "start_session", Arguments: map[string]any{}})
				if err != nil || door.IsError {
					t.Fatalf("start_session: %v %+v", err, door)
				}
				handle, _ := door.StructuredContent.(map[string]any)["session"].(string)
				info, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "info", Arguments: map[string]any{"session": handle}})
				if err != nil || info.IsError {
					t.Fatalf("info: %v %+v", err, info)
				}
				rules, _ := info.StructuredContent.(map[string]any)["secretRules"].(map[string]any)
				if rules["source"] != tc.source || rules["tag"] != tc.tag || rules["update"] != tc.update {
					t.Errorf("the served info should name the rule file the setting selects at serve start, got %v", rules)
				}
			})
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

// The error the command fails with is the only report: no warning repeats it.
func TestAnUnknownSecretRulesUpdateIsReportedOnce(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, "config/sdd/config.yaml", "participant: Global Author\nsecrets:\n  rules_update: weekly\n")

	result := f.run(t, "info")

	if result.err == nil {
		t.Fatalf("the command should fail, got stdout %q", result.stdout)
	}
	if strings.Contains(result.stderr, "weekly") {
		t.Errorf("no warning should repeat the error the command fails with, got %q", result.stderr)
	}
}
