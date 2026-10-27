package cliapp_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	writeFile(t, filepath.Join(root, ".gitignore"), ".sdd/tmp/\n.sdd/config.local.yaml\n.sdd/index/\n.sdd/stats/\n.sdd/sessions/\n.sdd/staged-blobs/\n.sdd/graph/.sdd-runtime/\n")
	gitIn(t, root, "add", ".gitignore", ".sdd/config.yaml")
	gitIn(t, root, "commit", "-q", "-m", "sdd: init")
	gitIn(t, root, "push", "-q")
	local := "participant: Ada\nsync:\n  cooldown: 1ns\n"
	if mode != "" {
		local += "  mode: " + mode + "\n"
	}
	writeFile(t, filepath.Join(root, ".sdd", "config.local.yaml"), local)
	return root
}

// upstreamCommit pushes a commit with the given subject from a second clone,
// as another participant would.
func upstreamCommit(t *testing.T, root, subject, file string) string {
	t.Helper()
	remote := strings.TrimSpace(gitIn(t, root, "remote", "get-url", "origin"))
	other := filepath.Join(canonicalTemp(t), "other")
	gitIn(t, filepath.Dir(other), "clone", "-q", remote, other)
	gitIn(t, other, "config", "user.email", "other@example.com")
	gitIn(t, other, "config", "user.name", "Other")
	writeFile(t, filepath.Join(other, file), subject+"\n")
	gitIn(t, other, "add", file)
	gitIn(t, other, "commit", "-q", "-m", subject)
	gitIn(t, other, "push", "-q")
	return strings.TrimSpace(gitIn(t, other, "rev-parse", "HEAD"))
}

func contains(t *testing.T, root, ancestor string) bool {
	t.Helper()
	cmd := exec.Command("git", "merge-base", "--is-ancestor", ancestor, "HEAD")
	cmd.Dir = root
	return cmd.Run() == nil
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

func TestUpstreamGraphCommitsArriveOnTheSyncCheck(t *testing.T) {
	root := syncedProject(t, "auto")
	theirs := upstreamCommit(t, root, "sdd: capture elsewhere", ".sdd/graph/elsewhere.md")

	out, err := runAppErr(t, root, captureArgs()...)
	if err != nil {
		t.Fatalf("capture should succeed: %v\n%s", err, out)
	}
	if !contains(t, root, theirs) {
		t.Error("the upstream graph commit should be merged before the command runs")
	}
	if !strings.HasPrefix(remoteHead(t, root), localHead(t, root)) {
		t.Error("the capture after the pull should reach the upstream")
	}
}

func TestDivergedGraphMergesWithAGraphCommit(t *testing.T) {
	root := syncedProject(t, "auto")
	writeFile(t, filepath.Join(root, ".sdd", "graph", "local.md"), "local\n")
	gitIn(t, root, "add", ".sdd/graph/local.md")
	gitIn(t, root, "commit", "-q", "-m", "sdd: capture here")
	theirs := upstreamCommit(t, root, "sdd: capture elsewhere", ".sdd/graph/elsewhere.md")

	out, err := runAppErr(t, root, "view", "--layout", "layer(tac):as-list")
	if err != nil {
		t.Fatalf("view should succeed: %v\n%s", err, out)
	}
	if !contains(t, root, theirs) {
		t.Fatalf("the upstream graph commit should be merged, got %q", out)
	}
	if subject := headSubject(t, root); !strings.HasPrefix(subject, "sdd: merge") {
		t.Errorf("the merge commit should carry a graph subject, got %q", subject)
	}
	if !strings.HasPrefix(remoteHead(t, root), localHead(t, root)) {
		t.Error("the merge should be published")
	}
}

func TestPullHeldAndReported(t *testing.T) {
	cases := []struct {
		name    string
		subject string
		file    string
		dirty   bool
		report  string
	}{
		{name: "a foreign commit upstream", subject: "feat: change code", file: "main.go", report: "not written by sdd"},
		{name: "a dirty working tree", subject: "sdd: capture elsewhere", file: ".sdd/graph/elsewhere.md", dirty: true, report: "uncommitted changes"},
		{name: "a graph commit changing code", subject: "sdd: capture elsewhere", file: "main.go", report: "changes main.go, outside the graph"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := syncedProject(t, "auto")
			theirs := upstreamCommit(t, root, tc.subject, tc.file)
			if tc.dirty {
				writeFile(t, filepath.Join(root, "seed.txt"), "edited\n")
			}
			before := localHead(t, root)

			out, err := runAppErr(t, root, "view", "--layout", "layer(tac):as-list")
			if err != nil {
				t.Fatalf("view should succeed: %v\n%s", err, out)
			}
			if localHead(t, root) != before || contains(t, root, theirs) {
				t.Error("nothing should be merged")
			}
			if !strings.Contains(out, tc.report) {
				t.Errorf("the held pull should be reported, got %q", out)
			}
		})
	}
}

func TestManualSyncNeverPulls(t *testing.T) {
	root := syncedProject(t, "manual")
	theirs := upstreamCommit(t, root, "sdd: capture elsewhere", ".sdd/graph/elsewhere.md")
	runApp(t, root, "view", "--layout", "layer(tac):as-list")
	if contains(t, root, theirs) {
		t.Error("manual sync should merge nothing")
	}
}

func TestConflictingUpstreamIsReportedAndNothingMerged(t *testing.T) {
	root := syncedProject(t, "auto")
	writeFile(t, filepath.Join(root, ".sdd", "graph", "shared.md"), "here\n")
	gitIn(t, root, "add", ".sdd/graph/shared.md")
	gitIn(t, root, "commit", "-q", "-m", "sdd: edit here")
	theirs := upstreamCommit(t, root, "sdd: edit elsewhere", ".sdd/graph/shared.md")
	before := localHead(t, root)

	out, err := runAppErr(t, root, "view", "--layout", "layer(tac):as-list")
	if err != nil {
		t.Fatalf("view should succeed: %v\n%s", err, out)
	}
	if localHead(t, root) != before || contains(t, root, theirs) {
		t.Error("a conflicting upstream should leave the branch as it was")
	}
	if status := gitIn(t, root, "status", "--porcelain"); status != "" {
		t.Errorf("the working tree should stay clean, got %q", status)
	}
	if !regexp.MustCompile(`graph merge would conflict in \S*shared\.md`).MatchString(out) {
		t.Errorf("the pull should report the conflict naming the path, got %q", out)
	}
}

// holdSyncCheck keeps the background check from fetching, so a push meets an
// upstream this checkout has not seen.
func holdSyncCheck(t *testing.T, root string) {
	t.Helper()
	writeFile(t, filepath.Join(root, ".sdd", "config.local.yaml"), "participant: Ada\nsync:\n  cooldown: 1h\n  mode: auto\n")
	writeFile(t, filepath.Join(root, ".sdd", "tmp", "last-fetch"), "")
}

func TestRejectedPushMergesUpstreamAndPushesAgain(t *testing.T) {
	root := syncedProject(t, "auto")
	holdSyncCheck(t, root)
	theirs := upstreamCommit(t, root, "sdd: capture elsewhere", ".sdd/graph/elsewhere.md")

	out, err := runAppErr(t, root, captureArgs()...)
	if err != nil {
		t.Fatalf("a push rejected for upstream graph commits should be retried: %v\n%s", err, out)
	}
	if !contains(t, root, theirs) {
		t.Error("the upstream graph commit should be merged by the retry")
	}
	if subject := headSubject(t, root); !strings.HasPrefix(subject, "sdd: merge") {
		t.Errorf("the retry should merge with a graph subject, got %q", subject)
	}
	if !strings.HasPrefix(remoteHead(t, root), localHead(t, root)) {
		t.Error("the capture should reach the upstream after the retry")
	}
}

func TestRejectedPushFailsWhenUpstreamCannotMerge(t *testing.T) {
	root := syncedProject(t, "auto")
	holdSyncCheck(t, root)
	upstreamCommit(t, root, "feat: change code", "main.go")

	out, err := runAppErr(t, root, captureArgs()...)
	if err == nil {
		t.Fatalf("a retry that cannot merge should fail the command, got %q", out)
	}
	if !strings.HasPrefix(headSubject(t, root), "sdd: signal") {
		t.Error("the capture commit should stay after the failed retry")
	}
	if !strings.Contains(out, "not written by sdd") {
		t.Errorf("the held merge should be reported, got %q", out)
	}
}
