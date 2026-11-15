package mcpapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

func TestStageAttachmentPathOverHTTPIsRefused(t *testing.T) {
	env := newTestServer(t, nil, "", "", useHTTPIdentity)
	endpoint := newTestHTTPServer(t, env.srv.Handler(), nil)
	cs := connect(t, env.srv, endpoint.Transport("tester"))
	session := openSession(t, cs).Session
	path := writeServerFile(t)

	msg := callExpectError(t, cs, "stage_attachment", map[string]any{"session": session, "name": "a.md", "path": path})
	if !strings.Contains(msg, "pass the file as content") {
		t.Fatalf("a path over HTTP should be refused with a pointer to content, got %q", msg)
	}
	if strings.Contains(msg, "a file of the server's") {
		t.Errorf("the refusal should not carry the file's content, got %q", msg)
	}
	callExpectError(t, cs, "read_attachment", map[string]any{"session": session, "handle": "a.md"})
}

func TestStageAttachmentPathOnALocalClientIsRead(t *testing.T) {
	env := newTestServer(t, nil, "", "", func(o *testServerOptions) { o.MCP.LocalClient = true })
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session
	path := writeServerFile(t)

	var staged mcpserver.StageAttachmentResult
	call(t, cs, "stage_attachment", map[string]any{"session": session, "name": "a.md", "path": path}, &staged)
	var page mcpserver.ReadAttachmentResult
	call(t, cs, "read_attachment", map[string]any{"session": session, "handle": staged.Handle}, &page)
	if page.Content != "a file of the server's\n" {
		t.Errorf("a local client should stage the file read from its path, got %q", page.Content)
	}
}

func writeServerFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "server-side.md")
	err := os.WriteFile(path, []byte("a file of the server's\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	return path
}
