package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --local-config replaces the machine-local layer for the whole run: reads
// resolve the passed file, and a `config set --local` write lands in it rather
// than in the repo's own .sdd/config.local.yaml.
func TestLocalConfigFlagRedirectsReadsAndWrites(t *testing.T) {
	f := newAppFixture(t)
	override := filepath.Join(t.TempDir(), "override-config.yaml")
	if err := os.WriteFile(override, []byte("participant: FromOverride\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := f.run(t, "--local-config", override, "config", "get", "participant")
	if !strings.Contains(result.stdout, "FromOverride") {
		t.Errorf("read side ignored the override: %s", result.stdout)
	}
	if strings.Contains(result.stdout, "Local Author") {
		t.Errorf("read side still consulted the in-repo layer: %s", result.stdout)
	}

	if setResult := f.run(t, "--local-config", override, "config", "set", "--local", "participant", "Ada"); setResult.err != nil {
		t.Fatalf("config set --local: %v\n%s", setResult.err, setResult.stderr)
	}

	written, err := os.ReadFile(override)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "Ada") {
		t.Errorf("write landed elsewhere: %s", written)
	}
	inRepo, err := os.ReadFile(filepath.Join(f.root, ".sdd", "config.local.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(inRepo), "Ada") {
		t.Errorf("the in-repo layer was written despite the override: %s", inRepo)
	}
}

// init records the participant in the override too, not in the repo's own
// layer.
func TestLocalConfigFlagRedirectsInitParticipant(t *testing.T) {
	f := newAppFixture(t)
	runGit(t, f.root, "init", "-q", "-b", "main")
	runGit(t, f.root, "config", "user.email", "test@example.com")
	runGit(t, f.root, "config", "user.name", "Test")
	runGit(t, f.root, "add", ".")
	runGit(t, f.root, "commit", "-q", "-m", "chore: seed")
	override := filepath.Join(t.TempDir(), "override-config.yaml")
	if err := os.WriteFile(override, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	result := f.run(t, "--local-config", override, "init", "--scope", "project", "--agents", "claude", "--participant", "Ada")
	if result.err != nil {
		t.Fatalf("init: %v\n%s", result.err, result.stderr)
	}

	written, err := os.ReadFile(override)
	if err != nil {
		t.Fatalf("init should write the participant to the override: %v", err)
	}
	if !strings.Contains(string(written), "Ada") {
		t.Errorf("the participant landed elsewhere: %s", written)
	}
	inRepo, err := os.ReadFile(filepath.Join(f.root, ".sdd", "config.local.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(inRepo), "Ada") {
		t.Errorf("the in-repo layer was written despite the override: %s", inRepo)
	}
}

// The layer is per invocation: a run without the flag must not inherit a
// previous run's override.
func TestLocalConfigOverrideDoesNotLeakBetweenRuns(t *testing.T) {
	f := newAppFixture(t)
	override := filepath.Join(t.TempDir(), "override-config.yaml")
	if err := os.WriteFile(override, []byte("participant: FromOverride\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	f.run(t, "--local-config", override, "config", "get", "participant")

	result := f.run(t, "config", "get", "participant")
	if !strings.Contains(result.stdout, "Local Author") {
		t.Errorf("a run without the flag must read the repo's own layer: %s", result.stdout)
	}
}

// A mistyped override fails the run instead of dropping the local layer.
func TestLocalConfigFlagNamingAMissingFileFails(t *testing.T) {
	f := newAppFixture(t)
	missing := filepath.Join(t.TempDir(), "absent.yaml")

	result := f.run(t, "--local-config", missing, "config")

	if result.err == nil || !strings.Contains(result.err.Error(), missing) {
		t.Fatalf("a missing override should fail naming its path, got %v", result.err)
	}
}

// An override inside a repository must be ignored there, so neither a tracked
// file nor a stray secret can reach a commit.
func TestLocalConfigFlagRefusesAFileARepositoryCouldCommit(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		refused bool
	}{
		{name: "tracked config", file: ".sdd/config.yaml", refused: true},
		{name: "untracked file not ignored", file: "secrets.yaml", refused: true},
		{name: "ignored file", file: "ignored.local.yaml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAppFixture(t)
			f.write(t, ".gitignore", "ignored.local.yaml\n")
			runGit(t, f.root, "init", "-q", "-b", "main")
			runGit(t, f.root, "add", ".sdd/config.yaml", ".gitignore")
			f.write(t, tc.file, "participant: FromOverride\n")
			before := readFile(t, filepath.Join(f.root, tc.file))

			result := f.run(t, "--local-config", filepath.Join(f.root, tc.file), "config", "set", "--local", "participant", "Bob")

			if !tc.refused {
				if result.err != nil {
					t.Fatalf("an ignored override should be accepted: %v", result.err)
				}
				return
			}
			if result.err == nil || !strings.Contains(result.err.Error(), "is not ignored") {
				t.Fatalf("an override a repository could commit should be refused, got %v", result.err)
			}
			if readFile(t, filepath.Join(f.root, tc.file)) != before {
				t.Error("a refused override should stay unchanged")
			}
		})
	}
}

// A symlink outside any repository is judged by the file it names.
func TestLocalConfigFlagRefusesALinkToATrackedFile(t *testing.T) {
	f := newAppFixture(t)
	runGit(t, f.root, "init", "-q", "-b", "main")
	runGit(t, f.root, "add", ".sdd/config.yaml")
	tracked := filepath.Join(f.root, ".sdd", "config.yaml")
	before := readFile(t, tracked)
	link := filepath.Join(t.TempDir(), "link.yaml")
	if err := os.Symlink(tracked, link); err != nil {
		t.Fatal(err)
	}

	result := f.run(t, "--local-config", link, "config", "set", "--local", "participant", "Mallory")

	if result.err == nil || !strings.Contains(result.err.Error(), "is not ignored") {
		t.Fatalf("a link to a tracked file should be refused, got %v", result.err)
	}
	if readFile(t, tracked) != before {
		t.Error("the tracked file should stay unchanged")
	}
}

// Writing a key starts a new override file, which reads then go through.
func TestLocalConfigSetStartsAMissingOverride(t *testing.T) {
	f := newAppFixture(t)
	override := filepath.Join(t.TempDir(), "new.yaml")

	if result := f.run(t, "--local-config", override, "config", "set", "--local", "participant", "Ada"); result.err != nil {
		t.Fatalf("config set --local should create the override: %v\n%s", result.err, result.stderr)
	}
	result := f.run(t, "--local-config", override, "config", "get", "participant")
	if result.err != nil || !strings.Contains(result.stdout, "Ada") {
		t.Errorf("the new override should be read: %v %s", result.err, result.stdout)
	}
}
