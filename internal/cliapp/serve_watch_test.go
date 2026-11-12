package cliapp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/test"
)

// callTool calls a tool and returns its structured result as JSON, or the
// error text the call ended with.
func callToolResult(t *testing.T, client *mcp.ClientSession, name string, args map[string]any) (string, string) {
	t.Helper()
	result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return "", err.Error()
	}
	if result.IsError {
		var text []string
		for _, content := range result.Content {
			if part, ok := content.(*mcp.TextContent); ok {
				text = append(text, part.Text)
			}
		}
		return "", strings.Join(text, "\n")
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	return test.Untagged(string(encoded)), ""
}

// withWaitingLanes follows every serve in result whose lanes wait past the
// result bound, calling next with no report until none does, and returns
// result with the text of each call appended.
func withWaitingLanes(t *testing.T, client *mcp.ClientSession, result string) string {
	t.Helper()
	type serve struct {
		Instance     string `json:"instance"`
		Instructions string `json:"instructions"`
	}
	var carrier struct {
		Session string  `json:"session"`
		Open    []serve `json:"open_instances"`
		serve
	}
	err := json.Unmarshal([]byte(result), &carrier)
	if err != nil {
		t.Fatal(err)
	}
	joined := result
	for _, s := range append(carrier.Open, carrier.serve) {
		for range 10 {
			if !strings.Contains(s.Instructions, "lanes of these instructions wait") {
				break
			}
			next, failure := callToolResult(t, client, "next", map[string]any{"session": carrier.Session, "instance": s.Instance})
			if failure != "" {
				t.Fatalf("next: %s", failure)
			}
			joined += "\n" + next
			s = serve{Instance: s.Instance}
			err = json.Unmarshal([]byte(next), &s)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	return joined
}

func openSession(t *testing.T, client *mcp.ClientSession) (string, string) {
	t.Helper()
	opening, failure := callToolResult(t, client, "start_session", map[string]any{})
	if failure != "" {
		t.Fatalf("start_session: %s", failure)
	}
	var door struct {
		Session string `json:"session"`
	}
	err := json.Unmarshal([]byte(opening), &door)
	if err != nil || door.Session == "" {
		t.Fatalf("start_session returned no handle: %v %s", err, opening)
	}
	return door.Session, withWaitingLanes(t, client, opening)
}

func hostNotice(t *testing.T, result string) string {
	t.Helper()
	var carrier struct {
		HostNotice string `json:"host_notice"`
	}
	err := json.Unmarshal([]byte(result), &carrier)
	if err != nil {
		t.Fatal(err)
	}
	return carrier.HostNotice
}

// A running session follows the dialogue mode a config layer sets while serve
// runs, and the next result names the change once.
func TestServeFollowsAConfigChangeWithoutAReconnect(t *testing.T) {
	fixture := newAppFixture(t)
	fixture.serve(t, func(client *mcp.ClientSession) {
		session, opening := openSession(t, client)
		if strings.Contains(opening, "working on your own") {
			t.Fatal("the session should open guided")
		}

		fixture.write(t, ".sdd/config.local.yaml", "participant: Local Author\ndialogue:\n  mode: autonomous\n")
		resumed, failure := callToolResult(t, client, "resume_session", map[string]any{"session": session})
		if failure != "" {
			t.Fatalf("resume_session: %s", failure)
		}
		if !strings.Contains(withWaitingLanes(t, client, resumed), "working on your own") {
			t.Errorf("the running session should follow the new dialogue mode:\n%s", resumed)
		}
		if notice := hostNotice(t, resumed); !strings.Contains(notice, "dialogue.mode: guided -> autonomous") {
			t.Errorf("the next result should name the changed key with its old and new value, got %q", notice)
		}

		again, failure := callToolResult(t, client, "resume_session", map[string]any{"session": session})
		if failure != "" {
			t.Fatalf("resume_session: %s", failure)
		}
		if notice := hostNotice(t, again); notice != "" {
			t.Errorf("a change should be told once, got %q", notice)
		}
	})
}

// A config file that appears after serve started counts, and so does one
// that vanishes.
func TestServeFollowsConfigFilesThatAppearOrVanish(t *testing.T) {
	fixture := newAppFixture(t)
	local := filepath.Join(fixture.root, ".sdd", "config.local.yaml")
	err := os.Remove(local)
	if err != nil {
		t.Fatal(err)
	}
	fixture.serve(t, func(client *mcp.ClientSession) {
		session, _ := openSession(t, client)

		fixture.write(t, ".sdd/config.local.yaml", "participant: Local Author\n")
		resumed, failure := callToolResult(t, client, "resume_session", map[string]any{"session": session})
		if failure != "" {
			t.Fatalf("resume_session: %s", failure)
		}
		if notice := hostNotice(t, resumed); !strings.Contains(notice, "participant: Global Author -> Local Author") {
			t.Errorf("a config file created while serving should take effect, got %q", notice)
		}

		err := os.Remove(local)
		if err != nil {
			t.Fatal(err)
		}
		resumed, failure = callToolResult(t, client, "resume_session", map[string]any{"session": session})
		if failure != "" {
			t.Fatalf("resume_session: %s", failure)
		}
		if notice := hostNotice(t, resumed); !strings.Contains(notice, "participant: Local Author -> Global Author") {
			t.Errorf("a config file removed while serving should stop taking effect, got %q", notice)
		}
	})
}

// A config file left broken refuses every call with its name and parse error
// until it is fixed.
func TestServeRefusesEveryCallWhileAConfigFileIsBroken(t *testing.T) {
	fixture := newAppFixture(t)
	committed := readFile(t, filepath.Join(fixture.root, ".sdd", "config.yaml"))
	fixture.serve(t, func(client *mcp.ClientSession) {
		session, _ := openSession(t, client)

		fixture.write(t, ".sdd/config.yaml", committed+"language: [\n")
		for range 2 {
			_, failure := callToolResult(t, client, "resume_session", map[string]any{"session": session})
			if !strings.Contains(failure, filepath.Join(".sdd", "config.yaml")) || !strings.Contains(failure, "yaml") {
				t.Errorf("a broken config should refuse the call naming the file and the parse error, got %q", failure)
			}
		}

		fixture.write(t, ".sdd/config.yaml", committed)
		_, failure := callToolResult(t, client, "resume_session", map[string]any{"session": session})
		if failure != "" {
			t.Errorf("a fixed config should serve again: %s", failure)
		}
	})
}

// A graph_dir change refuses every call while the new folder is missing or
// empty, or entries stay in the folder served before.
func TestServeRefusesCallsWhileTheGraphMoveIsUnsettled(t *testing.T) {
	fixture := newAppFixture(t)
	committed := readFile(t, filepath.Join(fixture.root, ".sdd", "config.yaml"))
	entry := filepath.Join("2026", "09", "14-020000-s-tac-cli.md")
	fixture.serve(t, func(client *mcp.ClientSession) {
		session, _ := openSession(t, client)
		resume := func() string {
			_, failure := callToolResult(t, client, "resume_session", map[string]any{"session": session})
			return failure
		}

		fixture.write(t, ".sdd/config.yaml", strings.Replace(committed, "graph_dir: decisions", "graph_dir: moved", 1))
		if failure := resume(); !strings.Contains(failure, "does not exist") {
			t.Errorf("a graph folder that does not exist should refuse the call, got %q", failure)
		}
		err := os.MkdirAll(filepath.Join(fixture.root, "moved"), 0o755)
		if err != nil {
			t.Fatal(err)
		}
		if failure := resume(); !strings.Contains(failure, "holds no entries") {
			t.Errorf("an empty graph folder should refuse the call, got %q", failure)
		}
		fixture.write(t, filepath.ToSlash(filepath.Join("moved", entry)), readFile(t, filepath.Join(fixture.root, "decisions", entry)))
		if failure := resume(); !strings.Contains(failure, "still in") {
			t.Errorf("entries left in the folder served before should refuse the call, got %q", failure)
		}
		err = os.Remove(filepath.Join(fixture.root, "decisions", entry))
		if err != nil {
			t.Fatal(err)
		}
		if failure := resume(); failure != "" {
			t.Errorf("a settled move should serve again: %s", failure)
		}
	})
}
