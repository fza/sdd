package cliapp_test

import (
	"path/filepath"
	"regexp"
	"testing"
)

// sdd config lists the hooks of both personal layers by the keys a person
// writes, each labelled with the layer it comes from; a key the hook leaves
// unset is not listed (20261109-005138-d-tac-i61).
func TestConfigListsTheHooksOfEveryPersonalLayer(t *testing.T) {
	f := newAppFixture(t)
	for file, command := range map[string]string{"config/sdd/config.yaml": "notify-global", ".sdd/config.local.yaml": "lint-local"} {
		existing := readFile(t, filepath.Join(f.root, file))
		f.write(t, file, existing+"hooks:\n  - event: entry.written\n    command: "+command+"\n")
	}

	result := f.run(t, "config")
	if result.err != nil {
		t.Fatalf("sdd config: %v\n%s", result.err, result.stderr)
	}
	for _, row := range []string{`hooks\.0\.event +entry\.written +\(global\)`, `hooks\.0\.command +notify-global +\(global\)`, `hooks\.1\.event +entry\.written +\(local\)`, `hooks\.1\.command +lint-local +\(local\)`} {
		if !regexp.MustCompile(row).MatchString(result.stdout) {
			t.Errorf("sdd config should list each layer's hook by its keys with its layer, matching %q:\n%s", row, result.stdout)
		}
	}
	if unset := regexp.MustCompile(`hooks\.\d+\.(timeout|findings)`).FindString(result.stdout); unset != "" {
		t.Errorf("sdd config should not list %s, which no hook sets:\n%s", unset, result.stdout)
	}
}
