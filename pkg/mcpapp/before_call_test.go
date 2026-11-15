package mcpapp_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	sdd "github.com/networkteam/sdd/pkg/application"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// scriptedBeforeCall answers each call with the next queued notice and error
// and records the session every call named.
type scriptedBeforeCall struct {
	mu       sync.Mutex
	notices  []string
	err      error
	sessions []string
}

func (h *scriptedBeforeCall) hook(_ context.Context, session string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessions = append(h.sessions, session)
	if h.err != nil {
		return "", h.err
	}
	if len(h.notices) == 0 {
		return "", nil
	}
	notice := h.notices[0]
	h.notices = h.notices[1:]
	return notice, nil
}

func (h *scriptedBeforeCall) queue(notices ...string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.notices = append(h.notices, notices...)
}

func (h *scriptedBeforeCall) refuse(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.err = err
}

func (h *scriptedBeforeCall) seen() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.sessions...)
}

func withBeforeCall(hook *scriptedBeforeCall) func(*testServerOptions) {
	return func(opts *testServerOptions) { opts.MCP.BeforeCall = hook.hook }
}

func callResult(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	return res
}

func TestBeforeCallNoticeRidesOnTheResult(t *testing.T) {
	hook := &scriptedBeforeCall{}
	env := newTestServer(t, nil, "", "", withBeforeCall(hook))
	cs := connect(t, env.srv)

	hook.queue("config changed, now in force:\n- language: de -> en")
	opened := callRaw(t, cs, "start_session", map[string]any{})
	if opened["host_notice"] != "config changed, now in force:\n- language: de -> en" {
		t.Errorf("the hook's notice should ride on the result, got %v", opened["host_notice"])
	}
	session, _ := opened["session"].(string)
	if session == "" {
		t.Fatalf("start_session served no session handle: %v", opened)
	}

	resumed := callRaw(t, cs, "resume_session", map[string]any{"session": " " + session + " "})
	if _, ok := resumed["host_notice"]; ok {
		t.Errorf("a call without a notice should omit host_notice, got %v", resumed["host_notice"])
	}
	if got := hook.seen(); len(got) != 2 || got[0] != "" || got[1] != session {
		t.Errorf("the hook should get the session each call names, none for start_session, got %q", got)
	}
}

func TestBeforeCallErrorRefusesTheCall(t *testing.T) {
	hook := &scriptedBeforeCall{}
	env := newTestServer(t, nil, "", "", withBeforeCall(hook))
	cs := connect(t, env.srv)

	hook.refuse(errors.New("loading .sdd/config.yaml: yaml: line 3: did not find expected node content"))
	res := callResult(t, cs, "start_session", map[string]any{})
	if !res.IsError || !strings.Contains(contentText(res), "loading .sdd/config.yaml: yaml: line 3") {
		t.Errorf("the hook's error should refuse the call with its text, got %q", contentText(res))
	}
	page, err := env.sessions.List(t.Context(), sdd.SessionFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Sessions) != 0 {
		t.Errorf("a refused call should not run its handler, found %d sessions", len(page.Sessions))
	}
}

func TestBeforeCallNoticeJoinsAFailedCallsError(t *testing.T) {
	hook := &scriptedBeforeCall{}
	env := newTestServer(t, nil, "", "", withBeforeCall(hook))
	cs := connect(t, env.srv)

	hook.queue("config changed, now in force:\n- language: de -> en")
	res := callResult(t, cs, "resume_session", map[string]any{"session": "s_20260101-000000-00000000000000000000000000000000"})
	if !res.IsError {
		t.Fatal("resuming an unknown session should fail")
	}
	if text := contentText(res); !strings.HasSuffix(text, "\n\nconfig changed, now in force:\n- language: de -> en") {
		t.Errorf("a failed call should carry the notice after its error, got %q", text)
	}
}

func TestBeforeCallCoversRegisteredTools(t *testing.T) {
	hook := &scriptedBeforeCall{}
	env := newTestServer(t, nil, "", "", withBeforeCall(hook))
	host := mcp.NewServer(&mcp.Implementation{Name: "host", Version: "test"}, nil)
	err := mcpserver.Register(host, env.opts)
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	_, err = host.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	cs := connect(t, nil, clientTransport)

	hook.queue("config changed, now in force:\n- language: de -> en")
	opened := callRaw(t, cs, "start_session", map[string]any{})
	if opened["host_notice"] != "config changed, now in force:\n- language: de -> en" {
		t.Errorf("a host registering SDD's tools should get the same hook, got %v", opened["host_notice"])
	}
}

func TestBeforeCallRunsForEveryTool(t *testing.T) {
	hook := &scriptedBeforeCall{}
	env := newTestServer(t, nil, "", "", withBeforeCall(hook))
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session

	res, err := cs.ListTools(t.Context(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	hook.refuse(errors.New("refused by the host"))
	for _, tool := range res.Tools {
		encoded, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Required   []string `json:"required"`
			Properties map[string]struct {
				Type json.RawMessage `json:"type"`
			} `json:"properties"`
		}
		err = json.Unmarshal(encoded, &schema)
		if err != nil {
			t.Fatal(err)
		}
		args := map[string]any{}
		if _, ok := schema.Properties["session"]; ok {
			args["session"] = session
		}
		for _, name := range schema.Required {
			if strings.Contains(string(schema.Properties[name].Type), "array") {
				args[name] = []string{"x"}
			} else if name != "session" {
				args[name] = "x"
			}
		}
		refused := callResult(t, cs, tool.Name, args)
		if !refused.IsError || !strings.Contains(contentText(refused), "refused by the host") {
			t.Errorf("%s should run the hook before its handler, got %q", tool.Name, contentText(refused))
		}
	}
}

// Every tool declares host_notice, so a notice reaches reads as well as steps.
func TestEveryToolDeclaresHostNotice(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)

	res, err := cs.ListTools(t.Context(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		encoded, err := json.Marshal(tool.OutputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Properties map[string]any `json:"properties"`
		}
		err = json.Unmarshal(encoded, &schema)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := schema.Properties["host_notice"]; !ok {
			t.Errorf("%s should declare host_notice in its output schema", tool.Name)
		}
	}
}
