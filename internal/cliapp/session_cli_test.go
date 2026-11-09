package cliapp_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/test"
)

const (
	sessionOtherID   = "20260914-030000-s-tac-two"
	sessionOtherBody = "Release notes leave out the migration steps of a breaking change."
	sessionThirdID   = "20260914-040000-s-tac-thr"
)

// sessionFixture is the app fixture as a committed git repository with a
// second entry and a scripted LLM, the grounding gate enforced or not.
func sessionFixture(t *testing.T, enforce bool) *appFixture {
	t.Helper()
	f := newAppFixture(t)
	llm := test.NewOpenAIServer(t, test.OpenAIBehaviour{Answer: `{"title": "Session capture", "summary": "A gap written into a session's capture.", "findings": []}`})
	config := "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\n"
	if enforce {
		config += "verify:\n  mode: grounding\n"
	} else {
		config += "verify:\n  mode: off\n"
	}
	f.write(t, ".sdd/config.yaml", config)
	f.write(t, ".sdd/config.local.yaml", "participant: Local Author\nllm:\n  provider: openai\n  model: fake\n  endpoint: "+llm.URL+"\n  api_keys:\n    openai: op-notarealkeyaaaaaaaaaa\n  retry:\n    max_attempts: 1\n")
	f.write(t, "decisions/2026/09/14-030000-s-tac-two.md", "---\ntype: signal\nkind: gap\nlayer: tactical\nconfidence: high\nparticipants: [Local Author]\nsummary: Release notes omit migration steps.\n---\n\n"+sessionOtherBody+"\n")
	f.write(t, "decisions/2026/09/14-040000-s-tac-thr.md", "---\ntype: signal\nkind: gap\nlayer: tactical\nconfidence: high\nparticipants: [Local Author]\nsummary: Onboarding lacks a glossary.\n---\n\nOnboarding material uses terms it never defines.\n")
	f.write(t, ".gitignore", "config/\ncache/\nstate/\n.sdd/config.local.yaml\n.sdd/tmp/\n")
	initRepo(t, f.root)
	gitIn(t, f.root, "add", ".gitignore", ".sdd/config.yaml", "decisions")
	gitIn(t, f.root, "commit", "-q", "-m", "graph")
	return f
}

// openCapture starts a session and a capture in it over the MCP server, the
// way an agent does, and returns the session handle.
func openCapture(t *testing.T, f *appFixture) string {
	t.Helper()
	var session string
	f.serve(t, func(client *mcp.ClientSession) {
		opened, served, failed := callTool(t, client, "start_session", map[string]any{})
		if failed {
			t.Fatalf("start_session: %s", served)
		}
		_, served, failed = callTool(t, client, "start_procedure", map[string]any{"session": opened.Session, "canonical": "capture"})
		if failed {
			t.Fatalf("start_procedure: %s", served)
		}
		session = opened.Session
	})
	return session
}

// openFixtureSession starts a session over the fixture's MCP server and
// returns its handle.
func openFixtureSession(t *testing.T, f *appFixture) string {
	t.Helper()
	var session string
	f.serve(t, func(client *mcp.ClientSession) {
		opened, served, failed := callTool(t, client, "start_session", map[string]any{})
		if failed {
			t.Fatalf("start_session: %s", served)
		}
		session = opened.Session
	})
	return session
}

// sessionLog is the stored event log of the session.
func sessionLog(t *testing.T, f *appFixture, session string) string {
	t.Helper()
	var found string
	err := filepath.WalkDir(filepath.Join(f.root, "state"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Name() == session+".jsonl" {
			found = path
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == "" {
		t.Fatalf("no stored log for session %s", session)
	}
	content, err := os.ReadFile(found)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func sessionNewArgs(session string) []string {
	return []string{"new", "--session", session, "--confidence", "high",
		"--refs", `{"id":"` + appEntryID + `","kind":"related"}`, "--topics", "testing/cli",
		"--widen-report", "searched the graph for configured reads",
		"s", "tac", "The CLI reads of a session lack a record of the graph directory they used."}
}

// CLI reads with --session log into the session, as the MCP reads do
// (20261109-004506-d-cpt-udz, D3).
func TestCLISessionReadsLogIntoTheSession(t *testing.T) {
	cases := []struct {
		name string
		args []string
		tool string
	}{
		{"show", []string{"show", appEntryID}, `"tool":"show"`},
		{"search", []string{"search", "--term", "configured graph directory"}, `"tool":"search"`},
		{"view", []string{"view", "--layout", `id("` + appEntryID + `"):as-bodies`}, `"tool":"view"`},
		{"gather", []string{"gather"}, `"tool":"gather"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := sessionFixture(t, false)
			session := openCapture(t, f)
			before := strings.Count(sessionLog(t, f, session), appEntryID)
			result := f.run(t, append(tc.args, "--session", session)...)
			if result.err != nil {
				t.Fatalf("sdd %s --session: %v\n%s", tc.name, result.err, result.stderr)
			}
			log := sessionLog(t, f, session)
			if !strings.Contains(log, tc.tool) || strings.Count(log, appEntryID) <= before {
				t.Fatalf("the session log should hold the %s read of %s:\n%s", tc.name, appEntryID, log)
			}
		})
	}
}

// A ref read through sdd show --session counts as inspected by the session's
// capture, so sdd new --session writes it.
func TestCLISessionShowInspectsARefForTheCapture(t *testing.T) {
	f := sessionFixture(t, false)
	session := openCapture(t, f)

	result := f.run(t, sessionNewArgs(session)...)
	if result.err == nil || !strings.Contains(result.err.Error(), "stopped at step assemble") {
		t.Fatalf("an uninspected ref should hold the capture at assemble: %v\n%s", result.err, result.stdout)
	}
	assertContainsOutput(t, "stderr", result.stderr, "refs not inspected", appEntryID)

	result = f.run(t, "show", "--session", session, appEntryID)
	if result.err != nil {
		t.Fatalf("sdd show --session: %v\n%s", result.err, result.stderr)
	}
	result = f.run(t, sessionNewArgs(session)...)
	if result.err != nil {
		t.Fatalf("the inspected ref should let the capture write: %v\n%s", result.err, result.stderr)
	}
	assertContainsOutput(t, "stdout", result.stdout, "-s-tac-", "Summary: A gap written into a session's capture.")
}

// Where verify.mode enforces grounding, sdd new --session passes the gate the
// engine capture passes: it holds the draft until a gather inside the capture read
// the graph and every entry it served in full is referenced or dismissed
// (D3, D9, D39).
func TestCLISessionNewPassesTheCaptureGate(t *testing.T) {
	f := sessionFixture(t, true)
	session := openCapture(t, f)

	result := f.run(t, sessionNewArgs(session)...)
	if result.err == nil || !strings.Contains(result.err.Error(), "stopped at step assemble") {
		t.Fatalf("a capture with no reading should stay at assemble: %v\n%s", result.err, result.stdout)
	}
	assertContainsOutput(t, "stderr", result.stderr, "no credited reading")

	result = f.run(t, "gather", "--session", session, appEntryID, sessionOtherID, sessionThirdID)
	if result.err != nil {
		t.Fatalf("sdd gather --session: %v\n%s", result.err, result.stderr)
	}
	result = f.run(t, sessionNewArgs(session)...)
	if result.err == nil {
		t.Fatalf("an entry read in full and left unanswered should hold the capture:\n%s", result.stdout)
	}
	assertContainsOutput(t, "stderr", result.stderr, sessionOtherID, sessionThirdID)

	for _, dismissed := range []string{sessionOtherID, sessionThirdID} {
		result = f.run(t, "dismiss", "--session", session, dismissed, "--because", "other-scope")
		if result.err != nil {
			t.Fatalf("sdd dismiss --session: %v\n%s", result.err, result.stderr)
		}
	}
	result = f.run(t, sessionNewArgs(session)...)
	if result.err != nil {
		t.Fatalf("the grounded draft should be written: %v\n%s", result.err, result.stderr)
	}
	assertContainsOutput(t, "stdout", result.stdout, "-s-tac-", "Summary: A gap written into a session's capture.")
}

// Inside a session the CLI refuses what the capture takes no field for, and
// names the capture it lacks.
func TestCLISessionRefusals(t *testing.T) {
	f := sessionFixture(t, false)
	withCapture := openCapture(t, f)
	withoutCapture := openFixtureSession(t, f)
	newArgs := func(session string, extra ...string) []string {
		return append([]string{"new", "--session", session, "--confidence", "high"}, append(extra, "s", "tac", "A gap.")...)
	}
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"new with a flag the capture has no field for", newArgs(withCapture, "--dry-run"), "--dry-run"},
		{"new with a kind of the other type", newArgs(withCapture, "--kind", "directive"), "not a signal kind"},
		{"gather with its own queries", []string{"gather", "--session", withCapture, "--query", "graph"}, "--query"},
		{"dismiss without a capture", []string{"dismiss", "--session", withoutCapture, sessionOtherID, "--because", "other-scope"}, "runs no capture"},
		{"new naming a capture the session lacks", newArgs(withCapture, "--instance", "i_99"), "runs no capture i_99"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := f.run(t, tc.args...)
			if result.err == nil || !strings.Contains(result.err.Error(), tc.want) {
				t.Fatalf("the call should be refused naming the cause: %v", result.err)
			}
		})
	}
}

// sdd dismiss and sdd new act only on a session's capture.
func TestCLISessionCommandsNeedASession(t *testing.T) {
	f := sessionFixture(t, false)
	for _, args := range [][]string{
		{"dismiss", sessionOtherID, "--because", "other-scope"},
		{"show", "--instance", "capture-1", appEntryID},
	} {
		result := f.run(t, args...)
		if result.err == nil || !strings.Contains(result.err.Error(), "--session") {
			t.Errorf("sdd %s should name --session: %v", args[0], result.err)
		}
	}
	result := f.run(t, "gather", "--session", "no-such-session")
	if result.err == nil {
		t.Error("an unknown session should be refused")
	}
}

// A CLI call with --session beside a running serve leaves the serve able to
// continue the session (20261109-055313-s-tac-7j7).
func TestCLISessionCallLeavesTheServeAbleToContinue(t *testing.T) {
	f := sessionFixture(t, false)
	f.serve(t, func(client *mcp.ClientSession) {
		opened, served, failed := callTool(t, client, "start_session", map[string]any{})
		if failed {
			t.Fatalf("start_session: %s", served)
		}
		capture, served, failed := callTool(t, client, "start_procedure", map[string]any{"session": opened.Session, "canonical": "capture"})
		if failed {
			t.Fatalf("start_procedure: %s", served)
		}
		result := f.run(t, "show", appEntryID, "--session", opened.Session)
		if result.err != nil {
			t.Fatalf("sdd show --session: %v\n%s", result.err, result.stderr)
		}
		_, served, failed = callTool(t, client, "park", map[string]any{"session": opened.Session, "instance": capture.Instance})
		if failed {
			t.Fatalf("the serve should continue the session after the CLI call:\n%s", served)
		}
	})
}
