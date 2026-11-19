package cliapp_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A project opened through a symlink to its folder reads the participant from
// its .sdd/config.local.yaml, as when opened by its real path.
func TestCLIProjectThroughSymlinkReadsLocalParticipant(t *testing.T) {
	for _, tc := range []struct {
		name string
		git  bool
	}{
		{"plain folder", false},
		{"git checkout", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			real := filepath.Join(base, "real")
			link := filepath.Join(base, "link")
			f := newAppFixture(t)
			f.root = real
			f.write(t, "config/sdd/config.yaml", "{}\n")
			f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\n")
			f.write(t, ".sdd/config.local.yaml", "participant: Local Author\n")
			f.write(t, "decisions/2026/09/14-020000-s-tac-cli.md", "---\ntype: signal\nkind: gap\nlayer: tactical\nconfidence: high\nparticipants: [Local Author]\nsummary: "+appSummary+"\n---\n\n"+appBody+"\n")
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(real, "config"))
			if tc.git {
				cmd := exec.Command("git", "init", "-q", "-b", "main", real)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git init: %v\n%s", err, out)
				}
			}
			if err := os.Symlink(real, link); err != nil {
				t.Fatal(err)
			}
			t.Chdir(link)

			for _, args := range [][]string{{"info"}, {"new", "--confidence", "high", "--skip-preflight", "--dry-run", "s", "tac", "A dry-run signal."}} {
				result := f.run(t, args...)
				if result.err != nil {
					t.Fatalf("%s: %v\n%s", args[0], result.err, result.stderr)
				}
				if strings.Contains(result.stderr, "no participant configured") {
					t.Errorf("%s through the symlink found no participant:\n%s", args[0], result.stderr)
				}
				if args[0] == "info" {
					assertContainsOutput(t, "stdout", result.stdout, "Local participant: Local Author")
				}
			}
		})
	}
}
