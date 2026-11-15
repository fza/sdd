package cliapp_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// sdd config lists the hooks of every layer by the keys a person writes,
// each labelled with the layer it comes from; a key the hook leaves unset is
// not listed.
func TestConfigListsTheHooksOfEveryLayer(t *testing.T) {
	f := newAppFixture(t)
	for _, layer := range []struct{ file, command string }{
		{"config/sdd/config.yaml", "notify-global"},
		{".sdd/config.yaml", "lint-committed"},
		{".sdd/config.local.yaml", "lint-local"},
	} {
		existing := readFile(t, filepath.Join(f.root, layer.file))
		f.write(t, layer.file, existing+"hooks:\n  - event: entry.written\n    command: "+layer.command+"\n")
	}

	result := f.run(t, "config")
	if result.err != nil {
		t.Fatalf("sdd config: %v\n%s", result.err, result.stderr)
	}
	for _, row := range []string{
		`hooks\.0\.event +entry\.written +\(global\)`, `hooks\.0\.command +notify-global +\(global\)`,
		`hooks\.1\.event +entry\.written +\(project\)`, `hooks\.1\.command +lint-committed +\(project\)`,
		`hooks\.2\.event +entry\.written +\(local\)`, `hooks\.2\.command +lint-local +\(local\)`,
	} {
		if !regexp.MustCompile(row).MatchString(result.stdout) {
			t.Errorf("sdd config should list each layer's hook by its keys with its layer, matching %q:\n%s", row, result.stdout)
		}
	}
	unset := regexp.MustCompile(`hooks\.\d+\.(args|timeout|findings)`).FindString(result.stdout)
	if unset != "" {
		t.Errorf("sdd config should not list %s, which no hook sets:\n%s", unset, result.stdout)
	}
}

// In a project_dir layout both repositories declare hooks, in both of their
// config files; sdd config lists every layer in order, work before project.
func TestConfigListsTheHooksOfBothRepositoriesOfAProjectDirLayout(t *testing.T) {
	work, project := pointedRepos(t)
	declare := func(repo, file, command string) {
		path := filepath.Join(repo, ".sdd", file)
		existing := ""
		raw, err := os.ReadFile(path)
		if err == nil {
			existing = string(raw)
		}
		writeFile(t, path, existing+"hooks:\n  - event: capture.assemble\n    command: "+command+"\n")
	}
	declare(work, "config.yaml", "work.sh")
	declare(work, "config.local.yaml", "local/work-local.sh")
	declare(project, "config.yaml", "project.sh")
	declare(project, "config.local.yaml", "local/project-local.sh")

	config, err := runAppErr(t, work, "config")
	if err != nil {
		t.Fatalf("sdd config: %v\n%s", err, config)
	}
	for _, row := range []string{`hooks\.0\.command +work\.sh +\(work\)`, `hooks\.1\.command +local/work-local\.sh +\(work local\)`, `hooks\.2\.command +project\.sh +\(project\)`, `hooks\.3\.command +local/project-local\.sh +\(local\)`} {
		if !regexp.MustCompile(row).MatchString(config) {
			t.Errorf("sdd config should list %s:\n%s", row, config)
		}
	}
}
