package cliapp_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBlockedSyncBannerPrintsOncePerCommand(t *testing.T) {
	root := syncedProject(t, "auto")
	writeFile(t, filepath.Join(root, "code.go"), "package code\n")
	gitIn(t, root, "add", "code.go")
	gitIn(t, root, "commit", "-q", "-m", "feat: add code")

	out, err := runAppErr(t, root, captureArgs()...)
	if err != nil {
		t.Fatalf("capture should succeed: %v\n%s", err, out)
	}
	if got := strings.Count(out, "sync blocked since"); got != 1 {
		t.Errorf("a capture behind a foreign commit should print the banner once, got %d in %q", got, out)
	}
	if strings.Contains(out, "▲") {
		t.Errorf("the banner should be a plain line, not a warning, got %q", out)
	}
	out, err = runAppErr(t, root, "view", "--layout", "layer(tac):as-list")
	if err != nil {
		t.Fatalf("view should succeed: %v\n%s", err, out)
	}
	if got := strings.Count(out, "sync blocked since"); got != 1 {
		t.Errorf("the next command should print the still-held push once, got %d in %q", got, out)
	}
}

func TestInfoShowsTheConflictRecord(t *testing.T) {
	root := syncedProject(t, "auto")
	writeFile(t, filepath.Join(root, ".sdd", "graph", "shared.md"), "here\n")
	gitIn(t, root, "add", ".sdd/graph/shared.md")
	gitIn(t, root, "commit", "-q", "-m", "sdd: edit here")
	destination := localHead(t, root)
	source := upstreamCommit(t, root, "sdd: edit elsewhere", ".sdd/graph/shared.md")

	out, err := runAppErr(t, root, "info")
	if err != nil {
		t.Fatalf("info should succeed: %v\n%s", err, out)
	}
	for _, want := range []string{"Sync blocked: conflict since", "Upstream: origin/", "Source: " + source, "Destination: " + destination, "Paths: .sdd/graph/shared.md", "git merge origin/"} {
		if !strings.Contains(out, want) {
			t.Errorf("info should show the record and its fix, missing %q in %q", want, out)
		}
	}
}

func TestExplicitSyncRunsWhilePaused(t *testing.T) {
	root := syncedProject(t, "auto")
	writeFile(t, filepath.Join(root, ".sdd", "graph", "shared.md"), "here\n")
	gitIn(t, root, "add", ".sdd/graph/shared.md")
	gitIn(t, root, "commit", "-q", "-m", "sdd: edit here")
	upstreamCommit(t, root, "sdd: edit elsewhere", ".sdd/graph/shared.md")
	runApp(t, root, "view", "--layout", "layer(tac):as-list")
	later := upstreamCommit(t, root, "sdd: capture later", ".sdd/graph/later.md")

	runApp(t, root, "view", "--layout", "layer(tac):as-list")
	if fetched(root, later) {
		t.Fatal("a paused check should not fetch")
	}
	out, err := runAppErr(t, root, "sync")
	if err != nil {
		t.Fatalf("an explicit sync should run while paused: %v\n%s", err, out)
	}
	if !fetched(root, later) {
		t.Error("an explicit sync should fetch while paused")
	}
	if !strings.Contains(out, "would conflict") {
		t.Errorf("an explicit sync should re-record the conflict that remains, got %q", out)
	}
}

// fetched reports whether the repository at root holds commit.
func fetched(root, commit string) bool {
	cmd := exec.Command("git", "cat-file", "-e", commit+"^{commit}")
	cmd.Dir = root
	return cmd.Run() == nil
}

func TestMissingGraphDirectoryNeverSyncsAnotherRepository(t *testing.T) {
	for _, mode := range []string{"manual", "auto"} {
		t.Run(mode, func(t *testing.T) {
			root := canonicalTemp(t)
			if err := os.MkdirAll(filepath.Join(root, ".sdd"), 0o755); err != nil {
				t.Fatal(err)
			}
			initRepo(t, root)
			writeFile(t, filepath.Join(root, ".sdd", "config.yaml"), "graph_dir: ../missing-graph-dir\nrepo_id: example.test/missing\nlanguage: en\n")
			writeFile(t, filepath.Join(root, ".sdd", "config.local.yaml"), "participant: Ada\nsync:\n  mode: "+mode+"\n")

			out, _ := runAppErr(t, root, "view", "--layout", "layer(tac):as-list")
			if strings.Contains(out, "no remote configured") {
				t.Errorf("sync should not report the enclosing repository's state as the graph's, got %q", out)
			}
			if mode == "auto" && !strings.Contains(out, "missing-graph-dir") {
				t.Errorf("automatic sync should report the missing graph repository, got %q", out)
			}
		})
	}
}

func TestSyncIntervalIsPerPersonAndFloored(t *testing.T) {
	root := canonicalTemp(t)
	if err := os.MkdirAll(filepath.Join(root, ".sdd", "graph"), 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, root)
	writeFile(t, filepath.Join(root, ".sdd", "config.yaml"), "graph_dir: .sdd/graph\nrepo_id: example.test/interval\nlanguage: en\nsync:\n  interval: 5m\n")
	writeFile(t, filepath.Join(root, ".sdd", "config.local.yaml"), "participant: Ada\n")

	out, err := runAppErr(t, root, "view", "--layout", "layer(tac):as-list")
	if err != nil {
		t.Fatalf("a committed interval should not stop a command: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ignoring config setting") || !strings.Contains(out, "sync.interval") || !strings.Contains(out, "config.yaml") {
		t.Errorf("a committed interval should be reported naming file and key, got %q", out)
	}
	for _, value := range []string{"30s", "often"} {
		if out, err := runAppErr(t, root, "config", "set", "--local", "sync.interval", value); err == nil {
			t.Errorf("config set should refuse interval %q, got %q", value, out)
		}
	}
	if out, err := runAppErr(t, root, "config", "set", "--local", "sync.interval", "2m"); err != nil {
		t.Errorf("config set should accept a valid interval: %v\n%s", err, out)
	}
}
