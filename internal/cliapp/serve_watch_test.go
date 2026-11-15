package cliapp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// toolOutcome is a tool call's structured result, or the error text the call
// ended with.
type toolOutcome struct {
	Session    string `json:"session"`
	Language   string `json:"language"`
	HostNotice string `json:"host_notice"`

	failure string
}

func callServed(t *testing.T, client *mcp.ClientSession, name string, args map[string]any) toolOutcome {
	t.Helper()
	result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return toolOutcome{failure: err.Error()}
	}
	if result.IsError {
		var text []string
		for _, content := range result.Content {
			if part, ok := content.(*mcp.TextContent); ok {
				text = append(text, part.Text)
			}
		}
		return toolOutcome{failure: strings.Join(text, "\n")}
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var outcome toolOutcome
	err = json.Unmarshal(encoded, &outcome)
	if err != nil {
		t.Fatal(err)
	}
	return outcome
}

func callServedOK(t *testing.T, client *mcp.ClientSession, name string, args map[string]any) toolOutcome {
	t.Helper()
	outcome := callServed(t, client, name, args)
	if outcome.failure != "" {
		t.Fatalf("%s: %s", name, outcome.failure)
	}
	return outcome
}

func openServedSession(t *testing.T, client *mcp.ClientSession) string {
	t.Helper()
	opened := callServedOK(t, client, "start_session", map[string]any{})
	if opened.Session == "" {
		t.Fatal("start_session returned no session handle")
	}
	return opened.Session
}

func readFixtureFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// A running session follows a key changed while serve runs, and the next
// result names the change once.
func TestServeFollowsAConfigChangeWithoutAReconnect(t *testing.T) {
	fixture := newAppFixture(t)
	committed := readFixtureFile(t, filepath.Join(fixture.root, ".sdd", "config.yaml"))
	fixture.serve(t, func(client *mcp.ClientSession) {
		session := openServedSession(t, client)

		fixture.write(t, ".sdd/config.yaml", strings.Replace(committed, "language: de", "language: en", 1))
		info := callServedOK(t, client, "info", map[string]any{"session": session})
		if info.Language != "en" {
			t.Errorf("the running session should follow the changed key, got language %q", info.Language)
		}
		if !strings.Contains(info.HostNotice, "- language: de -> en") {
			t.Errorf("the next result should name the changed key with its old and new value, got %q", info.HostNotice)
		}

		again := callServedOK(t, client, "info", map[string]any{"session": session})
		if again.HostNotice != "" {
			t.Errorf("a change should be told once, got %q", again.HostNotice)
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
		session := openServedSession(t, client)

		fixture.write(t, ".sdd/config.local.yaml", "participant: Local Author\n")
		resumed := callServedOK(t, client, "resume_session", map[string]any{"session": session})
		if !strings.Contains(resumed.HostNotice, "- participant: Global Author -> Local Author") {
			t.Errorf("a config file created while serving should take effect, got %q", resumed.HostNotice)
		}

		err := os.Remove(local)
		if err != nil {
			t.Fatal(err)
		}
		resumed = callServedOK(t, client, "resume_session", map[string]any{"session": session})
		if !strings.Contains(resumed.HostNotice, "- participant: Local Author -> Global Author") {
			t.Errorf("a config file removed while serving should stop taking effect, got %q", resumed.HostNotice)
		}
	})
}

// A config file that appears unreadable refuses calls like a broken one.
func TestServeRefusesCallsWhileAConfigFileIsUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file of mode 000")
	}
	fixture := newAppFixture(t)
	local := filepath.Join(fixture.root, ".sdd", "config.local.yaml")
	err := os.Remove(local)
	if err != nil {
		t.Fatal(err)
	}
	fixture.serve(t, func(client *mcp.ClientSession) {
		session := openServedSession(t, client)

		err := os.WriteFile(local, []byte("participant: Local Author\n"), 0o000)
		if err != nil {
			t.Fatal(err)
		}
		refused := callServed(t, client, "resume_session", map[string]any{"session": session})
		if !strings.Contains(refused.failure, "config.local.yaml") {
			t.Errorf("an unreadable config file should refuse the call naming the file, got %q", refused.failure)
		}

		err = os.Chmod(local, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		resumed := callServedOK(t, client, "resume_session", map[string]any{"session": session})
		if !strings.Contains(resumed.HostNotice, "- participant: Global Author -> Local Author") {
			t.Errorf("the file made readable should take effect, got %q", resumed.HostNotice)
		}
	})
}

// A config file left broken refuses every call with its name and parse error
// until it is fixed; the change made meanwhile is told once served again.
func TestServeRefusesEveryCallWhileAConfigFileIsBroken(t *testing.T) {
	fixture := newAppFixture(t)
	committed := readFixtureFile(t, filepath.Join(fixture.root, ".sdd", "config.yaml"))
	fixture.serve(t, func(client *mcp.ClientSession) {
		session := openServedSession(t, client)

		fixture.write(t, ".sdd/config.yaml", committed+"language: [\n")
		for range 2 {
			refused := callServed(t, client, "resume_session", map[string]any{"session": session})
			if !strings.Contains(refused.failure, filepath.Join(".sdd", "config.yaml")) || !strings.Contains(refused.failure, "yaml") {
				t.Errorf("a broken config should refuse the call naming the file and the parse error, got %q", refused.failure)
			}
		}

		fixture.write(t, ".sdd/config.yaml", strings.Replace(committed, "language: de", "language: en", 1))
		resumed := callServed(t, client, "resume_session", map[string]any{"session": session})
		if resumed.failure != "" {
			t.Fatalf("a fixed config should serve again: %s", resumed.failure)
		}
		if !strings.Contains(resumed.HostNotice, "- language: de -> en") {
			t.Errorf("the first call served again should name the change, got %q", resumed.HostNotice)
		}
	})
}

// A session opened before serve started is told a change made while serve
// runs.
func TestServeTellsASessionOpenedBeforeItStartedOfAChange(t *testing.T) {
	fixture := newAppFixture(t)
	committed := readFixtureFile(t, filepath.Join(fixture.root, ".sdd", "config.yaml"))
	var session string
	fixture.serve(t, func(client *mcp.ClientSession) {
		session = openServedSession(t, client)
	})

	fixture.serve(t, func(client *mcp.ClientSession) {
		fixture.write(t, ".sdd/config.yaml", strings.Replace(committed, "language: de", "language: en", 1))
		told := callServedOK(t, client, "info", map[string]any{"session": session})
		if !strings.Contains(told.HostNotice, "- language: de -> en") {
			t.Errorf("a session opened before serve started should be told the change, got %q", told.HostNotice)
		}
	})
}

// A session that made no call across several rebuilds is told each change
// on its next call.
func TestServeJoinsTheChangesASessionMissed(t *testing.T) {
	fixture := newAppFixture(t)
	committed := readFixtureFile(t, filepath.Join(fixture.root, ".sdd", "config.yaml"))
	fixture.serve(t, func(client *mcp.ClientSession) {
		idle := openServedSession(t, client)
		busy := openServedSession(t, client)

		fixture.write(t, ".sdd/config.yaml", strings.Replace(committed, "language: de", "language: en", 1))
		callServedOK(t, client, "info", map[string]any{"session": busy})
		fixture.write(t, ".sdd/config.local.yaml", "participant: Other Author\n")
		callServedOK(t, client, "info", map[string]any{"session": busy})

		told := callServedOK(t, client, "info", map[string]any{"session": idle})
		for _, change := range []string{"- language: de -> en", "- participant: Local Author -> Other Author"} {
			if !strings.Contains(told.HostNotice, change) {
				t.Errorf("a session should be told every change it missed, got %q", told.HostNotice)
			}
		}
	})
}

// Every session open at a change is told it once, on its own next call; a
// call naming no session is told it once too, and a session opened after
// the change is not told it.
func TestServeTellsEveryOpenSessionOfAChangeOnce(t *testing.T) {
	fixture := newAppFixture(t)
	committed := readFixtureFile(t, filepath.Join(fixture.root, ".sdd", "config.yaml"))
	fixture.serve(t, func(client *mcp.ClientSession) {
		first := openServedSession(t, client)
		second := openServedSession(t, client)

		fixture.write(t, ".sdd/config.yaml", strings.Replace(committed, "language: de", "language: en", 1))
		for _, session := range []string{first, second} {
			told := callServedOK(t, client, "info", map[string]any{"session": session})
			if !strings.Contains(told.HostNotice, "- language: de -> en") {
				t.Errorf("each open session should be told the change on its next call, got %q", told.HostNotice)
			}
			again := callServedOK(t, client, "info", map[string]any{"session": session})
			if again.HostNotice != "" {
				t.Errorf("a session should be told a change once, got %q", again.HostNotice)
			}
		}

		opened := callServedOK(t, client, "start_session", map[string]any{})
		if !strings.Contains(opened.HostNotice, "- language: de -> en") {
			t.Errorf("the next call naming no session should be told the change, got %q", opened.HostNotice)
		}
		later := callServedOK(t, client, "info", map[string]any{"session": opened.Session})
		if later.HostNotice != "" {
			t.Errorf("a session opened after the change should not be told it, got %q", later.HostNotice)
		}
		if again := callServedOK(t, client, "start_session", map[string]any{}); again.HostNotice != "" {
			t.Errorf("calls naming no session should be told a change once, got %q", again.HostNotice)
		}
	})
}
