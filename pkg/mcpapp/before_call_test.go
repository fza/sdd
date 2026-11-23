package mcpapp_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	sdd "github.com/networkteam/sdd/pkg/application"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

type pinnedKey struct{}

// scriptedBeforeCall answers each call with the next queued notice and error,
// pins the call's number in its context, and records the session every call
// named.
type scriptedBeforeCall struct {
	mu       sync.Mutex
	notices  []string
	err      error
	sessions []string
}

func (h *scriptedBeforeCall) hook(ctx context.Context, session string) (context.Context, mcpserver.Notices, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessions = append(h.sessions, session)
	if h.err != nil {
		return ctx, mcpserver.Notices{}, h.err
	}
	ctx = context.WithValue(ctx, pinnedKey{}, len(h.sessions))
	if len(h.notices) == 0 {
		return ctx, mcpserver.Notices{}, nil
	}
	notice := h.notices[0]
	h.notices = h.notices[1:]
	return ctx, mcpserver.Notices{Config: notice}, nil
}

// pinRecorder records the call number each principal resolution ran under.
type pinRecorder struct {
	sdd.AccessResolver
	mu   sync.Mutex
	pins []any
}

func (r *pinRecorder) ResolvePrincipal(ctx context.Context, identity sdd.RequestIdentity) (sdd.Principal, error) {
	r.mu.Lock()
	r.pins = append(r.pins, ctx.Value(pinnedKey{}))
	r.mu.Unlock()
	return r.AccessResolver.ResolvePrincipal(ctx, identity)
}

func TestBeforeCallContextReachesTheApplication(t *testing.T) {
	hook := &scriptedBeforeCall{}
	recorder := &pinRecorder{}
	env := newTestServer(t, nil, "", "", withBeforeCall(hook), func(opts *testServerOptions) {
		recorder.AccessResolver = opts.Application.Access
		opts.Application.Access = recorder
	})
	cs := connect(t, env.srv)

	openSession(t, cs)
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.pins) == 0 {
		t.Fatal("start_session should resolve its principal")
	}
	for _, pin := range recorder.pins {
		if pin != 1 {
			t.Errorf("the call should run under the context the hook returned, got %v", pin)
		}
	}
}

func TestBeforeCallWithoutAContextKeepsTheCalls(t *testing.T) {
	env := newTestServer(t, nil, "", "", func(opts *testServerOptions) {
		opts.MCP.BeforeCall = func(context.Context, string) (context.Context, mcpserver.Notices, error) {
			return nil, mcpserver.Notices{}, nil
		}
	})
	cs := connect(t, env.srv)

	res := callResult(t, cs, "start_session", map[string]any{})
	if res.IsError {
		t.Errorf("a hook returning no context should leave the call its own, got %q", contentText(res))
	}
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
	if got := noticeOf(opened, "config"); got != "config changed, now in force:\n- language: de -> en" {
		t.Errorf("the hook's notice should ride on the result under its kind, got %v", got)
	}
	session, _ := opened["session"].(string)
	if session == "" {
		t.Fatalf("start_session served no session handle: %v", opened)
	}

	resumed := callRaw(t, cs, "resume_session", map[string]any{"session": " " + session + " "})
	if _, ok := resumed["notices"]; ok {
		t.Errorf("a call without a notice should omit notices, got %v", resumed["notices"])
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
	if got := noticeOf(opened, "config"); got != "config changed, now in force:\n- language: de -> en" {
		t.Errorf("a host registering SDD's tools should get the same hook, got %v", got)
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
				Type  json.RawMessage `json:"type"`
				Items struct {
					Type json.RawMessage `json:"type"`
				} `json:"items"`
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
			property := schema.Properties[name]
			switch {
			// stage_attachment's files (staged-capture-bodies) are objects.
			case strings.Contains(string(property.Type), "array") && strings.Contains(string(property.Items.Type), "object"):
				args[name] = []map[string]any{{"name": "x", "content": "x"}}
			case strings.Contains(string(property.Type), "array"):
				args[name] = []string{"x"}
			case name != "session":
				args[name] = "x"
			}
		}
		refused := callResult(t, cs, tool.Name, args)
		if !refused.IsError || !strings.Contains(contentText(refused), "refused by the host") {
			t.Errorf("%s should run the hook before its handler, got %q", tool.Name, contentText(refused))
		}
	}
}

// Every tool declares notices.config, so a notice reaches reads as well as steps.
// Notices ride on the top-level result only, never on a serve nested in it.
// withOutputSchema lists each tool's output schema, which serve-token-cost
// leaves out of the listing by default.
func withOutputSchema(o *testServerOptions) { o.MCP.OutputSchema = true }

func TestEveryToolDeclaresNotices(t *testing.T) {
	env := newTestServer(t, nil, "", "", withOutputSchema)
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
			Required   []string `json:"required"`
			Properties map[string]struct {
				Required   []string       `json:"required"`
				Properties map[string]any `json:"properties"`
				Items      struct {
					Properties map[string]any `json:"properties"`
				} `json:"items"`
			} `json:"properties"`
		}
		err = json.Unmarshal(encoded, &schema)
		if err != nil {
			t.Fatal(err)
		}
		notices, ok := schema.Properties["notices"]
		if !ok {
			t.Errorf("%s should declare notices in its output schema", tool.Name)
			continue
		}
		if _, ok := notices.Properties["config"]; !ok {
			t.Errorf("%s should declare notices.config in its output schema", tool.Name)
		}
		if slices.Contains(schema.Required, "notices") || slices.Contains(notices.Required, "config") {
			t.Errorf("%s should leave notices and each of its kinds optional", tool.Name)
		}
		for name, property := range schema.Properties {
			if _, ok := property.Items.Properties["notices"]; ok {
				t.Errorf("%s should declare notices only on its top-level result, not in %s", tool.Name, name)
			}
		}
	}
}

// noticeOf returns the notice of kind a result carries, nil for none.
func noticeOf(result map[string]any, kind string) any {
	notices, _ := result["notices"].(map[string]any)
	return notices[kind]
}
