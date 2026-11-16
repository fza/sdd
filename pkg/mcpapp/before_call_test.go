package mcpapp_test

import (
	"context"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	sdd "github.com/networkteam/sdd/pkg/application"
)

type pinnedKey struct{}

// pinRecorder records the pin each principal resolution ran under.
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
	recorder := &pinRecorder{}
	env := newTestServer(t, nil, "", "", func(opts *testServerOptions) {
		recorder.AccessResolver = opts.Application.Access
		opts.Application.Access = recorder
		opts.MCP.BeforeCall = func(ctx context.Context) (context.Context, string, error) {
			return context.WithValue(ctx, pinnedKey{}, "admitted"), "", nil
		}
	})
	cs := connect(t, env.srv)

	openSession(t, cs)
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.pins) == 0 {
		t.Fatal("start_session should resolve its principal")
	}
	for _, pin := range recorder.pins {
		if pin != "admitted" {
			t.Errorf("the call should run under the context BeforeCall returned, got %v", pin)
		}
	}
}

func TestBeforeCallWithoutAContextKeepsTheCalls(t *testing.T) {
	env := newTestServer(t, nil, "", "", func(opts *testServerOptions) {
		opts.MCP.BeforeCall = func(context.Context) (context.Context, string, error) {
			return nil, "", nil
		}
	})
	cs := connect(t, env.srv)

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "start_session", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Errorf("a BeforeCall returning no context should leave the call its own, got %q", contentText(res))
	}
}
