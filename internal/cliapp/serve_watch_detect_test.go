package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A write that keeps the size and the mtime, as `cp -p`, `rsync -t` or a
// write within one mtime tick leaves it, is followed too.
func TestServeFollowsAChangeThatKeepsSizeAndMtime(t *testing.T) {
	fixture := newAppFixture(t)
	local := filepath.Join(fixture.root, ".sdd", "config.local.yaml")
	info, err := os.Stat(local)
	if err != nil {
		t.Fatal(err)
	}
	fixture.serve(t, func(client *mcp.ClientSession) {
		session, _ := openSession(t, client)

		fixture.write(t, ".sdd/config.local.yaml", "participant: Local Editor\n")
		err := os.Chtimes(local, info.ModTime(), info.ModTime())
		if err != nil {
			t.Fatal(err)
		}
		resumed, failure := callToolResult(t, client, "resume_session", map[string]any{"session": session})
		if failure != "" {
			t.Fatalf("resume_session: %s", failure)
		}
		if notice := hostNotice(t, resumed); !strings.Contains(notice, "participant: Local Author -> Local Editor") {
			t.Errorf("a same-size write keeping the mtime should take effect, got %q", notice)
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
		session, _ := openSession(t, client)

		err := os.Remove(local)
		if err != nil {
			t.Fatal(err)
		}
		err = os.Symlink(filepath.Join(fixture.root, "profiles", "b.yaml"), local)
		if err != nil {
			t.Fatal(err)
		}
		resumed, failure := callToolResult(t, client, "resume_session", map[string]any{"session": session})
		if failure != "" {
			t.Fatalf("resume_session: %s", failure)
		}
		if notice := hostNotice(t, resumed); !strings.Contains(notice, "participant: Local Author -> Local Editor") {
			t.Errorf("a retargeted symlink should take effect, got %q", notice)
		}
	})
}

// A config file that appears unreadable refuses calls like a broken one,
// until it reads.
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
		session, _ := openSession(t, client)

		err := os.WriteFile(local, []byte("participant: Local Author\n"), 0o000)
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			_, failure := callToolResult(t, client, "resume_session", map[string]any{"session": session})
			if !strings.Contains(failure, "config.local.yaml") {
				t.Errorf("an unreadable config file should refuse the call naming the file, got %q", failure)
			}
		}

		err = os.Chmod(local, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		resumed, failure := callToolResult(t, client, "resume_session", map[string]any{"session": session})
		if failure != "" {
			t.Fatalf("resume_session: %s", failure)
		}
		if notice := hostNotice(t, resumed); !strings.Contains(notice, "participant: Global Author -> Local Author") {
			t.Errorf("the file made readable should take effect, got %q", notice)
		}
	})
}
