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
