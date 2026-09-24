package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --local-config replaces the machine-local layer for the whole run: reads
// resolve the passed file, and a `config set --local` write lands in it
// rather than in the repo's own .sdd/config.local.yaml.
func TestLocalConfigFlagRedirectsReadsAndWrites(t *testing.T) {
	root := canonicalTempDir(t)
	sddDir := filepath.Join(root, ".sdd")
	if err := os.MkdirAll(filepath.Join(sddDir, "graph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sddDir, "config.yaml"), []byte("graph_dir: .sdd/graph\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inRepoLocal := filepath.Join(sddDir, "config.local.yaml")
	if err := os.WriteFile(inRepoLocal, []byte("participant: InRepo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	override := filepath.Join(root, "override-config.yaml")
	if err := os.WriteFile(override, []byte("participant: FromOverride\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := runSDD(t, root, "--local-config", override, "config", "get", "participant")
	if !strings.Contains(out, "FromOverride") {
		t.Errorf("read side ignored the override: %s", out)
	}
	if strings.Contains(out, "InRepo") {
		t.Errorf("read side still consulted the in-repo layer: %s", out)
	}

	runSDD(t, root, "--local-config", override, "config", "set", "--local", "participant", "Ada")

	written, err := os.ReadFile(override)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "participant: Ada") {
		t.Errorf("write did not land in the override file: %s", written)
	}
	untouched, err := os.ReadFile(inRepoLocal)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(untouched), "participant: InRepo") {
		t.Errorf("the in-repo layer must stay untouched: %s", untouched)
	}
}

// runSDD re-enters main() in a subprocess with the given argv, rooted at
// dir and pointed at throwaway XDG locations so the developer's own global
// config never reaches the assertions.
func runSDD(t *testing.T, dir string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^$")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+filepath.Join(dir, "xdg-config"),
		"XDG_CACHE_HOME="+filepath.Join(dir, "xdg-cache"),
		mainHelperArgsEnv+"="+strings.Join(args, " "),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sdd %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
