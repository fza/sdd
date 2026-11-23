package cliapp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCLIServeOverHTTPRefusesStagingByPath(t *testing.T) {
	f := newAppFixture(t)
	path := filepath.Join(t.TempDir(), "server-side.md")
	err := os.WriteFile(path, []byte("a file of the server's\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	f.serveHTTP(t, func(client *mcp.ClientSession) {
		door, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "start_session", Arguments: map[string]any{}})
		if err != nil || door.IsError {
			t.Fatalf("start_session: %v %+v", err, door)
		}
		var opened struct {
			Session string `json:"session"`
		}
		raw, err := json.Marshal(door.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		err = json.Unmarshal(raw, &opened)
		if err != nil || opened.Session == "" {
			t.Fatalf("start_session returned no session: %s (%v)", raw, err)
		}

		staged, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "stage_attachment", Arguments: map[string]any{
			"session": opened.Session, "name": "a.md", "path": path,
		}})
		if err != nil {
			t.Fatal(err)
		}
		if !staged.IsError || !strings.Contains(toolText(staged), "pass the file as content") {
			t.Errorf("sdd serve over HTTP should refuse staging by path, got error=%v %q", staged.IsError, toolText(staged))
		}
	})
}

// serveHTTP runs sdd serve --transport http on a free local port and connects
// an MCP client carrying the bearer token.
func (f *appFixture) serveHTTP(t *testing.T, check func(*mcp.ClientSession)) {
	t.Helper()
	const token = "cli-test-token"
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	err = listener.Close()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	command := f.command(strings.NewReader(""), &bytes.Buffer{}, &stderr)
	done := make(chan error, 1)
	go func() {
		done <- command.Run(ctx, []string{"sdd", "serve", "--transport", "http", "--addr", addr, "--auth-token", token})
	}()
	defer func() {
		cancel()
		select {
		case runErr := <-done:
			if runErr != nil {
				t.Errorf("sdd serve: %v\n%s", runErr, stderr.String())
			}
		case <-time.After(10 * time.Second):
			t.Error("sdd serve did not stop after its context ended")
		}
	}()

	httpClient := &http.Client{Transport: bearerTransport{token: token}}
	var client *mcp.ClientSession
	for client == nil {
		transport := &mcp.StreamableClientTransport{Endpoint: "http://" + addr, HTTPClient: httpClient}
		client, err = mcp.NewClient(&mcp.Implementation{Name: "cli-test", Version: "test"}, nil).Connect(ctx, transport, nil)
		if err != nil && ctx.Err() != nil {
			t.Fatalf("connecting to sdd serve over HTTP: %v\n%s", err, stderr.String())
		}
		if err != nil {
			time.Sleep(50 * time.Millisecond)
		}
	}
	check(client)
	err = client.Close()
	if err != nil {
		t.Error(err)
	}
}

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(request)
}

func toolText(result *mcp.CallToolResult) string {
	var text strings.Builder
	for _, content := range result.Content {
		if part, ok := content.(*mcp.TextContent); ok {
			text.WriteString(part.Text)
		}
	}
	return text.String()
}
