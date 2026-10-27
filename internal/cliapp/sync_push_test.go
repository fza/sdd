package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// syncedProject builds a single-repository project whose branch tracks a
// local bare remote, with the given sync.mode in its local config.
func syncedProject(t *testing.T, mode string) string {
	t.Helper()
	base := canonicalTemp(t)
	root := filepath.Join(base, "project")
	remote := filepath.Join(base, "remote.git")
	if err := os.MkdirAll(filepath.Join(root, ".sdd", "graph"), 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, root)
	gitIn(t, base, "init", "-q", "--bare", remote)
	gitIn(t, root, "remote", "add", "origin", remote)
	gitIn(t, root, "push", "-q", "-u", "origin", "HEAD")
	writeFile(t, filepath.Join(root, ".sdd", "config.yaml"), "graph_dir: .sdd/graph\nrepo_id: example.test/synced\nlanguage: en\n")
	local := "participant: Ada\n"
	if mode != "" {
		local += "sync:\n  mode: " + mode + "\n"
	}
	writeFile(t, filepath.Join(root, ".sdd", "config.local.yaml"), local)
	return root
}

func remoteHead(t *testing.T, root string) string {
	t.Helper()
	return strings.TrimSpace(gitIn(t, root, "ls-remote", "origin", "HEAD"))
}

func localHead(t *testing.T, root string) string {
	t.Helper()
	return strings.TrimSpace(gitIn(t, root, "rev-parse", "HEAD"))
}

func captureArgs() []string {
	return []string{
		"new", "signal", "tactical",
		"The work repository carries only the work, while signals and decisions live in a project beside it.",
		"--confidence", "high", "--skip-preflight",
		"--summary", "A project beside the work keeps the work history free of capture commits.",
	}
}

func TestCaptureReachesUpstreamUnderAutoSync(t *testing.T) {
	root := syncedProject(t, "auto")
	out, err := runAppErr(t, root, captureArgs()...)
	if err != nil {
		t.Fatalf("capture should succeed: %v\n%s", err, out)
	}
	if !strings.HasPrefix(remoteHead(t, root), localHead(t, root)) {
		t.Error("the capture commit should reach the upstream before the command returns")
	}
}

func TestCaptureStaysLocalUnderManualSync(t *testing.T) {
	for _, mode := range []string{"", "manual"} {
		t.Run("mode "+mode, func(t *testing.T) {
			root := syncedProject(t, mode)
			before := remoteHead(t, root)
			runApp(t, root, captureArgs()...)
			if remoteHead(t, root) != before {
				t.Error("manual sync should push nothing")
			}
		})
	}
}

func TestCaptureHeldBehindAForeignCommit(t *testing.T) {
	root := syncedProject(t, "auto")
	writeFile(t, filepath.Join(root, "code.go"), "package code\n")
	gitIn(t, root, "add", "code.go")
	gitIn(t, root, "commit", "-q", "-m", "feat: add code")
	before := remoteHead(t, root)

	out, err := runAppErr(t, root, captureArgs()...)
	if err != nil {
		t.Fatalf("capture should succeed: %v\n%s", err, out)
	}
	if remoteHead(t, root) != before {
		t.Error("a commit not written by sdd ahead of upstream should keep everything local")
	}
	if !strings.Contains(out, "captured but unpublished") {
		t.Errorf("the held capture should be reported, got %q", out)
	}
}

func TestCaptureWithoutUpstreamIsReported(t *testing.T) {
	root := syncedProject(t, "auto")
	gitIn(t, root, "branch", "--unset-upstream")

	out, err := runAppErr(t, root, captureArgs()...)
	if err != nil {
		t.Fatalf("capture should succeed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "captured but unpublished") || !strings.Contains(out, "no upstream") {
		t.Errorf("a branch without upstream should be reported, got %q", out)
	}
}

func TestFailedPushFailsTheCaptureAndKeepsTheCommit(t *testing.T) {
	root := syncedProject(t, "auto")
	gitIn(t, root, "remote", "set-url", "origin", filepath.Join(filepath.Dir(root), "missing.git"))

	out, err := runAppErr(t, root, captureArgs()...)
	if err == nil {
		t.Fatalf("a failed push should fail the command, got output %q", out)
	}
	if !strings.HasPrefix(headSubject(t, root), "sdd: ") {
		t.Error("the capture commit should stay after a failed push")
	}
	if len(graphEntries(t, filepath.Join(root, ".sdd", "graph"))) != 1 {
		t.Error("the entry file should stay after a failed push")
	}
}
