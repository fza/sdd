package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A write that keeps the size and the mtime, as `cp -p`, `rsync -t` or a
// write within one mtime tick leaves it, is followed too.
func TestServeFollowsAChangeThatKeepsSizeAndMtime(t *testing.T) {
	fixture := newAppFixture(t)
	path := filepath.Join(fixture.root, ".sdd", "config.yaml")
	committed := readFixtureFile(t, path)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	fixture.serve(t, func(client *mcp.ClientSession) {
		session := openServedSession(t, client)

		fixture.write(t, ".sdd/config.yaml", strings.Replace(committed, "language: de", "language: en", 1))
		err := os.Chtimes(path, info.ModTime(), info.ModTime())
		if err != nil {
			t.Fatal(err)
		}
		served := callServedOK(t, client, "info", map[string]any{"session": session})
		if served.Language != "en" || !strings.Contains(served.HostNotice, "- language: de -> en") {
			t.Errorf("a same-size write keeping the mtime should take effect, got language %q, notice %q", served.Language, served.HostNotice)
		}
	})
}

// A config symlink retargeted to a file of the same size and mtime, as a
// Nix store switch does, is followed too.
func TestServeFollowsARetargetedConfigSymlink(t *testing.T) {
	fixture := newAppFixture(t)
	local := filepath.Join(fixture.root, ".sdd", "config.local.yaml")
	fixture.write(t, "profiles/a.yaml", "participant: Local Author\n")
	fixture.write(t, "profiles/b.yaml", "participant: Local Editor\n")
	stamp := time.Unix(1, 0)
	for _, name := range []string{"a.yaml", "b.yaml"} {
		err := os.Chtimes(filepath.Join(fixture.root, "profiles", name), stamp, stamp)
		if err != nil {
			t.Fatal(err)
		}
	}
	err := os.Remove(local)
	if err != nil {
		t.Fatal(err)
	}
	err = os.Symlink(filepath.Join(fixture.root, "profiles", "a.yaml"), local)
	if err != nil {
		t.Fatal(err)
	}
	fixture.serve(t, func(client *mcp.ClientSession) {
		session := openServedSession(t, client)

		err := os.Remove(local)
		if err != nil {
			t.Fatal(err)
		}
		err = os.Symlink(filepath.Join(fixture.root, "profiles", "b.yaml"), local)
		if err != nil {
			t.Fatal(err)
		}
		served := callServedOK(t, client, "info", map[string]any{"session": session})
		if !strings.Contains(served.HostNotice, "- participant: Local Author -> Local Editor") {
			t.Errorf("a retargeted symlink should take effect, got %q", served.HostNotice)
		}
	})
}

// A value in the wrong place, which may be a secret, never reaches the agent
// in the refusal.
func TestServeRefusalNeverShowsAConfigValue(t *testing.T) {
	fixture := newAppFixture(t)
	fixture.serve(t, func(client *mcp.ClientSession) {
		session := openServedSession(t, client)

		fixture.write(t, ".sdd/config.local.yaml", "participant: Local Author\nllm:\n  api_keys: sk-proj-notarealsecret\n")
		refused := callServed(t, client, "info", map[string]any{"session": session})
		if refused.failure == "" {
			t.Fatal("a scalar where a map belongs should refuse the call")
		}
		if strings.Contains(refused.failure, "sk-") {
			t.Errorf("the refusal should not show the value: %s", refused.failure)
		}
		if !strings.Contains(refused.failure, "llm.api_keys") || !strings.Contains(refused.failure, "line 3") {
			t.Errorf("the refusal should name the key and the line: %s", refused.failure)
		}
	})
}

// Calls running while a change is followed each see the new config, and
// every session is told the change once.
func TestServeFollowsAChangeUnderConcurrentCalls(t *testing.T) {
	fixture := newAppFixture(t)
	committed := readFixtureFile(t, filepath.Join(fixture.root, ".sdd", "config.yaml"))
	fixture.serve(t, func(client *mcp.ClientSession) {
		sessions := make([]string, 6)
		for i := range sessions {
			sessions[i] = openServedSession(t, client)
		}

		fixture.write(t, ".sdd/config.yaml", strings.Replace(committed, "language: de", "language: en", 1))
		outcomes := make([][]toolOutcome, len(sessions))
		var wg sync.WaitGroup
		for i, session := range sessions {
			wg.Go(func() {
				for range 3 {
					outcomes[i] = append(outcomes[i], callServed(t, client, "info", map[string]any{"session": session}))
				}
			})
		}
		wg.Wait()

		for _, calls := range outcomes {
			told := 0
			for _, outcome := range calls {
				if outcome.failure != "" {
					t.Fatalf("a call during a change should be served: %s", outcome.failure)
				}
				if outcome.Language != "en" {
					t.Errorf("every call after the change should see it, got language %q", outcome.Language)
				}
				if strings.Contains(outcome.HostNotice, "- language: de -> en") {
					told++
				}
			}
			if told != 1 {
				t.Errorf("each session should be told the change once, told %d times", told)
			}
		}
	})
}
