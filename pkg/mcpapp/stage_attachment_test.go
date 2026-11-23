package mcpapp_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	sdd "github.com/networkteam/sdd/pkg/application"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// failingStagedBlobs fails every write of one filename, so a call meets a
// write failure after its check passed.
type failingStagedBlobs struct {
	sdd.StagedBlobStore
	filename string
}

func (f failingStagedBlobs) Stage(ctx context.Context, ref sdd.SessionRef, filename string, content io.Reader) (sdd.StagedBlob, error) {
	if filename == f.filename {
		return sdd.StagedBlob{}, errors.New("disk full")
	}
	return f.StagedBlobStore.Stage(ctx, ref, filename, content)
}

func sha256Hex(content string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
}

// stagedOf returns the handles a read finds staged, in the order given.
func stagedOf(t *testing.T, cs *mcp.ClientSession, session string, handles ...string) []string {
	t.Helper()
	var staged []string
	for _, handle := range handles {
		res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "read_attachment", Arguments: map[string]any{"session": session, "handle": handle}})
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError {
			staged = append(staged, handle)
		}
	}
	return staged
}

// stdioClient serves a client sharing the server's filesystem, the only one
// that may stage by path (stage-path-local-only).
func stdioClient(o *testServerOptions) { o.MCP.LocalClient = true }

func TestStageAttachmentStagesEveryFileInCallOrder(t *testing.T) {
	env := newTestServer(t, nil, "", "", stdioClient)
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session
	path := filepath.Join(t.TempDir(), "from-path.md")
	err := os.WriteFile(path, []byte("read from a path\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	call(t, cs, "stage_attachment", map[string]any{"session": session, "files": []map[string]any{{"name": "patched.md", "content": "alpha beta\n"}}}, new(mcpserver.StageAttachmentResult))

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "stage_attachment", Arguments: map[string]any{
		"session": session,
		"files": []map[string]any{
			{"name": "inline.md", "content": "given inline\n"},
			{"name": "from-path.md", "path": path},
			{"name": "patched.md", "patches": []map[string]any{{"old": "beta", "new": "BETA"}}},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var staged mcpserver.StageAttachmentResult
	decodeStructured(t, res, &staged)
	want := []mcpserver.StagedFileResult{
		{Handle: "inline.md", SHA256: sha256Hex("given inline\n")},
		{Handle: "from-path.md", SHA256: sha256Hex("read from a path\n")},
		{Handle: "patched.md", SHA256: sha256Hex("alpha BETA\n")},
	}
	if fmt.Sprint(staged.StagedFiles()) != fmt.Sprint(want) {
		t.Fatalf("one call should stage content, path and patches files with one result each in call order, got %+v", staged.StagedFiles())
	}
	snapshotResponse(t, res)
}

func TestStageAttachmentRefusesTheWholeCallOnAFailingFile(t *testing.T) {
	tests := []struct {
		name   string
		second map[string]any
		reason string
	}{
		{name: "path unreadable", second: map[string]any{"name": "b.md", "path": "/nonexistent/b.md"}, reason: "reading /nonexistent/b.md"},
		{name: "patches not applying", second: map[string]any{"name": "base.md", "patches": []map[string]any{{"old": "absent", "new": "x"}}}, reason: "pair 1"},
		{name: "patches on an unstaged file", second: map[string]any{"name": "b.md", "patches": []map[string]any{{"old": "a", "new": "b"}}}, reason: "no file is staged"},
		{name: "two sources", second: map[string]any{"name": "b.md", "content": "x", "path": "/tmp/b.md"}, reason: "exactly one of content, path, or patches"},
		{name: "no source", second: map[string]any{"name": "b.md"}, reason: "exactly one of content, path, or patches"},
		{name: "name with a path", second: map[string]any{"name": "dir/b.md", "content": "x"}, reason: "plain filename"},
		{name: "same name twice", second: map[string]any{"name": "a.md", "content": "again"}, reason: "item 1 stages the same name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestServer(t, nil, "", "", stdioClient)
			cs := connect(t, env.srv)
			session := openSession(t, cs).Session
			call(t, cs, "stage_attachment", map[string]any{"session": session, "files": []map[string]any{{"name": "base.md", "content": "base\n"}}}, new(mcpserver.StageAttachmentResult))

			msg := callExpectError(t, cs, "stage_attachment", map[string]any{
				"session": session,
				"files":   []map[string]any{{"name": "a.md", "content": "first\n"}, tt.second, {"name": "c.md", "content": "third\n"}},
			})
			name, _ := tt.second["name"].(string)
			if !strings.Contains(msg, fmt.Sprintf("files item 2 %q", name)) || !strings.Contains(msg, tt.reason) || !strings.Contains(msg, "nothing was staged") {
				t.Fatalf("a failing second file should refuse the call naming it, got %q", msg)
			}
			if handles := stagedOf(t, cs, session, "base.md", "a.md", "c.md"); fmt.Sprint(handles) != "[base.md]" {
				t.Errorf("a refused call should stage none of its files, got %v", handles)
			}
		})
	}
}

func TestStageAttachmentRefusesAPathOnAServerSharingNoFilesystem(t *testing.T) {
	env := newTestServer(t, nil, "", "", stdioClient, func(o *testServerOptions) {
		o.Application.ReadLocalFile = nil
	})
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session
	path := filepath.Join(t.TempDir(), "server-side.md")
	err := os.WriteFile(path, []byte("a file of the server's\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	msg := callExpectError(t, cs, "stage_attachment", map[string]any{"session": session, "files": []map[string]any{{"name": "a.md", "path": path}}})
	if !strings.Contains(msg, `files item 1 "a.md"`) || !strings.Contains(msg, "pass the file as content") {
		t.Fatalf("a path should be refused when the server shares no filesystem with its clients, got %q", msg)
	}
	if handles := stagedOf(t, cs, session, "a.md"); len(handles) != 0 {
		t.Errorf("a refused path should stage nothing, got %v", handles)
	}
}

func TestStageAttachmentRefusesACallWithoutFiles(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session

	msg := callExpectError(t, cs, "stage_attachment", map[string]any{"session": session, "files": []map[string]any{}})
	if !strings.Contains(msg, "files is empty") {
		t.Fatalf("a call staging no file should be refused, got %q", msg)
	}
}

// A client written for the one-file call shape, name with content, path or
// patches beside session, stages that file and finds its handle at the top
// level of the answer.
func TestStageAttachmentTakesTheOneFileCallShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "from-path.md")
	err := os.WriteFile(path, []byte("read from a path\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "content", args: map[string]any{"name": "inline.md", "content": "given inline\n"}, want: "given inline\n"},
		{name: "path", args: map[string]any{"name": "from-path.md", "path": path}, want: "read from a path\n"},
		{name: "patches", args: map[string]any{"name": "base.md", "patches": []map[string]any{{"old": "beta", "new": "BETA"}}}, want: "alpha BETA\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestServer(t, nil, "", "", stdioClient)
			cs := connect(t, env.srv)
			session := openSession(t, cs).Session
			call(t, cs, "stage_attachment", map[string]any{"session": session, "files": []map[string]any{{"name": "base.md", "content": "alpha beta\n"}}}, new(mcpserver.StageAttachmentResult))
			args := map[string]any{"session": session}
			for key, value := range tt.args {
				args[key] = value
			}

			var staged mcpserver.StageAttachmentResult
			call(t, cs, "stage_attachment", args, &staged)
			name := tt.args["name"].(string)
			if staged.Handle != name {
				t.Errorf("a one-file call should answer its handle at the top level, got %q", staged.Handle)
			}
			want := []mcpserver.StagedFileResult{{Handle: name, SHA256: sha256Hex(tt.want)}}
			if fmt.Sprint(staged.StagedFiles()) != fmt.Sprint(want) {
				t.Errorf("a one-file call should stage that one file, got %+v", staged.StagedFiles())
			}
			var read mcpserver.ReadAttachmentResult
			call(t, cs, "read_attachment", map[string]any{"session": session, "handle": name}, &read)
			if read.Content != tt.want {
				t.Errorf("the staged file should hold %q, got %q", tt.want, read.Content)
			}
		})
	}
}

func TestStageAttachmentOneFileShapeStagesAnEmptyFile(t *testing.T) {
	tests := []struct {
		name string
		args func(session string) any
	}{
		{name: "empty content", args: func(session string) any {
			return map[string]any{"session": session, "name": "empty.md", "content": ""}
		}},
		{name: "name alone", args: func(session string) any {
			return map[string]any{"session": session, "name": "empty.md"}
		}},
		{name: "typed args", args: func(session string) any {
			return mcpserver.StageAttachmentArgs{Session: session, Name: "empty.md"}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestServer(t, nil, "", "")
			cs := connect(t, env.srv)
			session := openSession(t, cs).Session

			res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "stage_attachment", Arguments: tt.args(session)})
			checkToolCall(t, "stage_attachment", res, err)
			var staged mcpserver.StageAttachmentResult
			decodeStructured(t, res, &staged)
			if staged.Handle != "empty.md" {
				t.Fatalf("a one-file call without a source should stage an empty file, got %+v", staged)
			}
			var read mcpserver.ReadAttachmentResult
			call(t, cs, "read_attachment", map[string]any{"session": session, "handle": "empty.md"}, &read)
			if read.Content != "" {
				t.Errorf("the staged file should be empty, got %q", read.Content)
			}
		})
	}
}

func TestStageAttachmentAnswersAFilesCallWithoutATopLevelHandle(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session

	res := callRaw(t, cs, "stage_attachment", map[string]any{"session": session, "files": []map[string]any{{"name": "a.md", "content": "x"}}})
	if _, present := res["handle"]; present {
		t.Fatalf("a files call should answer per file only, got %v", res)
	}
}

func TestStageAttachmentRefusesBothCallShapesInOneCall(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session

	msg := callExpectError(t, cs, "stage_attachment", map[string]any{
		"session": session, "name": "a.md", "content": "x",
		"files": []map[string]any{{"name": "b.md", "content": "y"}},
	})
	if !strings.Contains(msg, "files") || !strings.Contains(msg, "not both") {
		t.Fatalf("a call mixing both shapes should be refused naming them, got %q", msg)
	}
	if handles := stagedOf(t, cs, session, "a.md", "b.md"); len(handles) != 0 {
		t.Errorf("a refused call should stage nothing, got %v", handles)
	}
}

func TestStageAttachmentListingShowsOnlyTheFilesShape(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)

	tools, err := cs.ListTools(t.Context(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != "stage_attachment" {
			continue
		}
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var input struct {
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(schema, &input); err != nil {
			t.Fatal(err)
		}
		keys := slices.Sorted(maps.Keys(input.Properties))
		if fmt.Sprint(keys) != "[files session]" {
			t.Fatalf("the listing should show only session and files, got %v", keys)
		}
		return
	}
	t.Fatal("stage_attachment is not listed")
}

func TestStageAttachmentWriteFailureReportsWhatStayedStaged(t *testing.T) {
	env := newTestServer(t, nil, "", "", func(o *testServerOptions) {
		o.Application.StagedBlobs = failingStagedBlobs{StagedBlobStore: o.Application.StagedBlobs, filename: "b.md"}
	})
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "stage_attachment", Arguments: map[string]any{
		"session": session,
		"files": []map[string]any{
			{"name": "a.md", "content": "first\n"},
			{"name": "b.md", "content": "second\n"},
			{"name": "c.md", "content": "third\n"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	msg := contentText(res)
	if !res.IsError || !strings.Contains(msg, `files item 2 "b.md"`) || !strings.Contains(msg, "this item was not staged") || !strings.Contains(msg, `staged before it and still staged: "a.md"`) {
		t.Fatalf("a write failing after the check should name the item, its state and the files staged before it, got %q", msg)
	}
	if handles := stagedOf(t, cs, session, "a.md", "b.md", "c.md"); fmt.Sprint(handles) != "[a.md]" {
		t.Errorf("the files staged before the failure should stay staged and none after it, got %v", handles)
	}
	snapshotResponse(t, res)
}
