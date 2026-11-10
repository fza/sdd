package cliapp_test

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const appCommittedConfig = "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\n"

// `sdd config get verify.kinds` is read by other tools to detect the
// per-kind modes, so its three shapes stay pinned: unset and an empty map
// fail with the same message, one entry prints the provenance table.
func TestConfigGetVerifyKindsOutput(t *testing.T) {
	for _, tc := range []struct {
		name, verify, stdout, failure string
	}{
		{name: "unset", failure: `no value for "verify.kinds"`},
		{name: "empty map", verify: "verify:\n  kinds: {}\n", failure: `no value for "verify.kinds"`},
		{name: "one entry", verify: "verify:\n  kinds:\n    done: deep\n", stdout: "verify.kinds.done  deep  (project)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAppFixture(t)
			f.write(t, ".sdd/config.yaml", appCommittedConfig+tc.verify)
			result := f.run(t, "config", "get", "verify.kinds")
			if tc.failure != "" {
				if result.err == nil || result.err.Error() != tc.failure || result.stdout != "" {
					t.Fatalf("an unset kinds map should fail with its fixed message and print nothing, got %v and %q", result.err, result.stdout)
				}
				return
			}
			if result.err != nil || result.stdout != tc.stdout {
				t.Fatalf("one entry should print the fixed provenance table, got %v and %q", result.err, result.stdout)
			}
		})
	}
}

// The served capture and `sdd new` take the mode of the draft's kind: a gap
// set to off owes no grounding reading where verify.mode enforces one.
func TestVerifyKindsSetTheModeOfACapture(t *testing.T) {
	for _, tc := range []struct {
		name, verify string
		grounded     bool
	}{
		{name: "verify.mode alone", verify: "verify:\n  mode: grounding\n", grounded: true},
		{name: "the kind set to off", verify: "verify:\n  mode: grounding\n  kinds:\n    gap: off\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAppFixture(t)
			f.write(t, ".sdd/config.yaml", appCommittedConfig+tc.verify)
			result := f.run(t, "new", "--confidence", "high", "--skip-preflight", "s", "tac", "An ungrounded gap.")
			refused := result.err != nil && strings.Contains(result.err.Error(), "enforces grounding")
			if refused != tc.grounded {
				t.Errorf("sdd new should owe a grounding reading only where the gap's mode enforces one, got %v", result.err)
			}
			f.serve(t, func(client *mcp.ClientSession) {
				opened, served, failed := callTool(t, client, "start_session", map[string]any{})
				if failed {
					t.Fatalf("start_session: %s", served)
				}
				serve, served, failed := callTool(t, client, "start_procedure", map[string]any{"session": opened.Session, "canonical": "capture"})
				if failed {
					t.Fatalf("start_procedure: %s", served)
				}
				serve, served, _ = callTool(t, client, "next", map[string]any{"session": opened.Session, "instance": serve.Instance, "report": captureReport()})
				if (serve.Step == "playback") == tc.grounded {
					t.Errorf("an ungathered capture should reach playback only where the gap's mode enforces no grounding, got step %q:\n%s", serve.Step, served)
				}
			})
		})
	}
}

func TestConfigSetVerifyKinds(t *testing.T) {
	f := newAppFixture(t)
	result := f.run(t, "config", "set", "--local", "verify.kinds.gapp", "deep")
	if result.err == nil || !strings.Contains(result.err.Error(), `"gapp" is not an entry kind`) {
		t.Fatalf("a name that is no entry kind should be refused, got %v", result.err)
	}
	result = f.run(t, "config", "set", "--local", "verify.kinds.done", "deeep")
	if result.err == nil || !strings.Contains(result.err.Error(), `"deeep" is not`) {
		t.Fatalf("an unknown mode should be refused, got %v", result.err)
	}
	result = f.run(t, "config", "set", "--local", "verify.kinds.done", "deep")
	if result.err != nil {
		t.Fatalf("a kind's mode should be set: %v", result.err)
	}
	got := f.run(t, "config", "get", "verify.kinds.done")
	if got.err != nil || got.stdout != "deep\n" {
		t.Fatalf("the kind's mode should read back, got %v and %q", got.err, got.stdout)
	}
}
