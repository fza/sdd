package cliapp_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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
	writeFile(t, filepath.Join(root, ".gitignore"), "xdg-config/\nxdg-cache/\nxdg-state/\n.sdd/tmp/\n.sdd/config.local.yaml\n.sdd/index/\n.sdd/stats/\n.sdd/sessions/\n.sdd/staged-blobs/\n.sdd/graph/.sdd-runtime/\n")
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

// A graph inside the code repository never syncs automatically, whatever
// sync.mode says, so a capture stays local however the branch stands.
func TestCaptureStaysLocalInTheCodeRepository(t *testing.T) {
	cases := []struct {
		name    string
		mode    string
		prepare func(t *testing.T, root string)
	}{
		{name: "mode unset"},
		{name: "mode manual", mode: "manual"},
		{name: "mode auto", mode: "auto"},
		{name: "mode auto behind a foreign commit", mode: "auto", prepare: func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, "code.go"), "package code\n")
			gitIn(t, root, "add", "code.go")
			gitIn(t, root, "commit", "-q", "-m", "feat: add code")
		}},
		{name: "mode auto without upstream", mode: "auto", prepare: func(t *testing.T, root string) {
			gitIn(t, root, "branch", "--unset-upstream")
		}},
		{name: "mode auto with the remote gone", mode: "auto", prepare: func(t *testing.T, root string) {
			gitIn(t, root, "remote", "set-url", "origin", filepath.Join(filepath.Dir(root), "missing.git"))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := syncedProject(t, tc.mode)
			bare := strings.TrimSpace(gitIn(t, root, "remote", "get-url", "origin"))
			before := gitIn(t, root, "ls-remote", bare, "HEAD")
			if tc.prepare != nil {
				tc.prepare(t, root)
			}

			out, err := runAppErr(t, root, captureArgs()...)
			if err != nil {
				t.Fatalf("capture should succeed: %v\n%s", err, out)
			}
			if gitIn(t, root, "ls-remote", bare, "HEAD") != before {
				t.Error("a capture in the code repository should push nothing")
			}
			if strings.Contains(out, "sync blocked") || strings.Contains(out, "graph commits stay unpublished") {
				t.Errorf("a graph without automatic sync should report no blocked sync, got %q", out)
			}
			if !strings.HasPrefix(headSubject(t, root), "sdd: ") {
				t.Error("the capture should commit locally")
			}
		})
	}
}

// The background check still fetches a graph inside the code repository and
// reports what the upstream holds, but never merges it.
func TestUpstreamGraphCommitsAreFetchedNeverMerged(t *testing.T) {
	cases := []struct {
		name    string
		subject string
		file    string
		local   bool
	}{
		{name: "a graph commit", subject: "sdd: capture elsewhere", file: ".sdd/graph/elsewhere.md"},
		{name: "a graph commit beside a local one", subject: "sdd: capture elsewhere", file: ".sdd/graph/elsewhere.md", local: true},
		{name: "a foreign commit", subject: "feat: change code", file: "main.go"},
		{name: "a graph commit changing code", subject: "sdd: capture elsewhere", file: "main.go"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := syncedProject(t, "auto")
			if tc.local {
				writeFile(t, filepath.Join(root, ".sdd", "graph", "local.md"), "local\n")
				gitIn(t, root, "add", ".sdd/graph/local.md")
				gitIn(t, root, "commit", "-q", "-m", "sdd: capture here")
			}
			theirs := upstreamCommit(t, root, tc.subject, tc.file)
			before := localHead(t, root)

			out, err := runAppErr(t, root, "view", "--layout", "layer(tac):as-list")
			if err != nil {
				t.Fatalf("view should succeed: %v\n%s", err, out)
			}
			if localHead(t, root) != before || contains(t, root, theirs) {
				t.Error("nothing upstream should be merged into the code repository")
			}
			if !fetched(root, theirs) {
				t.Error("the background check should still fetch the upstream")
			}
		})
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
	if !regexp.MustCompile(`sync: merge would conflict in \S*shared\.md`).MatchString(out) {
		t.Errorf("the status line should name the conflicting path, got %q", out)
	}
	if strings.Contains(out, "automatic sync is paused") {
		t.Errorf("a graph without automatic sync should record no pause, got %q", out)
	}
}

// A served session leaves a graph inside the code repository as it is: only a
// graph clone syncs automatically.
func TestServedSessionLeavesTheCodeRepositoryGraph(t *testing.T) {
	root := syncedProject(t, "auto")
	branch := strings.TrimSpace(gitIn(t, root, "rev-parse", "--abbrev-ref", "HEAD"))
	writeFile(t, filepath.Join(root, ".sdd", "config.yaml"), "graph_dir: .sdd/graph\nrepo_id: example.test/synced\nlanguage: en\ndefault_branch: "+branch+"\n")
	gitIn(t, root, "commit", "-q", "-am", "sdd: name the default branch")
	gitIn(t, root, "push", "-q")
	t.Chdir(root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".sdd", "tmp", "xdg-config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, ".sdd", "tmp", "xdg-cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".sdd", "tmp", "xdg-data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, ".sdd", "tmp", "xdg-state"))
	fixture := &appFixture{root: root}

	fixture.serve(t, func(client *mcp.ClientSession) {
		door, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "start_session", Arguments: map[string]any{}})
		if err != nil || door.IsError {
			t.Fatalf("start_session: %v %+v", err, door)
		}
		handle, _ := door.StructuredContent.(map[string]any)["session"].(string)
		theirs := upstreamCommit(t, root, "sdd: capture elsewhere", ".sdd/graph/elsewhere.md")
		info, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "info", Arguments: map[string]any{"session": handle}})
		if err != nil || info.IsError {
			t.Fatalf("info: %v %+v", err, info)
		}
		if contains(t, root, theirs) {
			t.Error("a session call should merge nothing into a graph inside the code repository")
		}
	})
}

// workRepoState is what a sync must never change in a work repository: HEAD,
// every ref including remote-tracking ones, and the index.
func workRepoState(t *testing.T, work string) string {
	t.Helper()
	return gitIn(t, work, "rev-parse", "HEAD") + "\n" +
		gitIn(t, work, "for-each-ref", "--format=%(refname) %(objectname)") + "\n" +
		gitIn(t, work, "ls-files", "--stage")
}

// A project kept in a repository of its own beside the work is no graph clone:
// it never syncs automatically, and the work repository stays untouched.
func TestPointedProjectNeverSyncsAutomatically(t *testing.T) {
	work, project := pointedRepos(t)
	base := filepath.Dir(work)
	for _, repo := range []string{work, project} {
		remote := filepath.Join(base, filepath.Base(repo)+".git")
		gitIn(t, base, "init", "-q", "--bare", remote)
		gitIn(t, repo, "remote", "add", "origin", remote)
	}
	writeFile(t, filepath.Join(work, ".gitignore"), "xdg-config/\nxdg-cache/\nxdg-state/\n")
	gitIn(t, work, "add", ".gitignore", ".sdd/config.yaml")
	gitIn(t, work, "commit", "-q", "-m", "chore: point at the project")
	gitIn(t, work, "push", "-q", "-u", "origin", "HEAD")
	writeFile(t, filepath.Join(project, ".gitignore"), ".sdd/tmp/\n.sdd/config.local.yaml\n.sdd/index/\n.sdd/stats/\n.sdd/sessions/\n.sdd/staged-blobs/\n.sdd/graph/.sdd-runtime/\n")
	gitIn(t, project, "add", ".gitignore", ".sdd/config.yaml")
	gitIn(t, project, "commit", "-q", "-m", "sdd: init")
	gitIn(t, project, "push", "-q", "-u", "origin", "HEAD")
	writeFile(t, filepath.Join(project, ".sdd", "config.local.yaml"), "participant: Ada\nsync:\n  cooldown: 1ns\n  mode: auto\n")
	upstreamCommit(t, work, "feat: work elsewhere", "elsewhere.go")
	theirs := upstreamCommit(t, project, "sdd: capture elsewhere", ".sdd/graph/elsewhere.md")
	before := workRepoState(t, work)
	published := remoteHead(t, project)

	out, err := runAppErr(t, work, captureArgs()...)
	if err != nil {
		t.Fatalf("capture: %v\n%s", err, out)
	}

	if contains(t, project, theirs) {
		t.Error("the project repository should merge nothing on its own")
	}
	if remoteHead(t, project) != published {
		t.Error("the project repository should push nothing on its own")
	}
	if workRepoState(t, work) != before {
		t.Error("a sync should leave the work repository's HEAD, refs and index unchanged")
	}
}

func TestCaptureIntoAnotherGraphStaysLocal(t *testing.T) {
	root := syncedProject(t, "auto")
	other := syncedProject(t, "manual")
	before := remoteHead(t, other)

	out, err := runAppErr(t, root, append(captureArgs(), "--graph-dir", filepath.Join(other, ".sdd", "graph"))...)
	if err != nil {
		t.Fatalf("capture: %v\n%s", err, out)
	}
	if remoteHead(t, other) != before {
		t.Error("a capture into a graph outside the project should not push under the project's sync mode")
	}
}

// deferFetches makes the gate's record hold the next fetch of the code
// repository's origin, moved to a host that is never reached.
func deferFetches(t *testing.T, root string) {
	t.Helper()
	gitIn(t, root, "remote", "set-url", "origin", "https://example.invalid/acme/graph.git")
	writeFile(t, filepath.Join(root, "xdg-state", "sdd", "remotes.json"),
		`{"version":1,"buckets":{},"repos":{"example.invalid/acme/graph":{"protocol":"https","fetch":{"retries_used":0,"next":"2099-01-01T00:00:00Z"}}}}`)
}

func TestCommandedSyncReportsADeferredPull(t *testing.T) {
	root := syncedProject(t, "")
	deferFetches(t, root)

	out, err := runAppErr(t, root, "sync", "--pull")
	if err != nil {
		t.Fatalf("a deferred pull should be a skip, not a failure: %v\n%s", err, out)
	}
	if !strings.Contains(out, "sync: pull skipped, next allowed at") {
		t.Errorf("the commanded sync should say it skipped and when it may run, got %q", out)
	}
}

func TestBackgroundCheckKeepsADeferralSilent(t *testing.T) {
	root := syncedProject(t, "")
	deferFetches(t, root)

	out, err := runAppErr(t, root, "view", "--layout", "layer(tac):as-list")
	if err != nil {
		t.Fatalf("view should succeed: %v\n%s", err, out)
	}
	if strings.Contains(out, "sync") {
		t.Errorf("a deferred background fetch should print nothing, got %q", out)
	}
}
