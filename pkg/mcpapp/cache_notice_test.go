package mcpapp_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// onceNotice hands out a notice once, as the host's record of a failed
// pruning run does.
type onceNotice struct {
	mu     sync.Mutex
	notice string
}

func (n *onceNotice) take(context.Context) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	notice := n.notice
	n.notice = ""
	return notice
}

// beforeCall is the host's BeforeCall, handing the notice out as notices.cache.
func (n *onceNotice) beforeCall(ctx context.Context, _ string) (context.Context, mcpserver.Notices, error) {
	return ctx, mcpserver.Notices{Cache: n.take(ctx)}, nil
}

func (n *onceNotice) set(notice string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.notice = notice
}

func TestTheCacheNoticeRidesOneResponse(t *testing.T) {
	notice := &onceNotice{notice: "background cache pruning failed at 2026-11-01T12:00:00Z: permission denied; see sdd info"}
	env := newTestServer(t, nil, "", "", func(o *testServerOptions) { o.MCP.BeforeCall = notice.beforeCall })
	client := connect(t, env.srv)
	var opened mcpserver.ServeResponse
	call(t, client, "start_session", map[string]any{}, &opened)
	if !strings.Contains(opened.Notices.Cache, "permission denied") {
		t.Errorf("the first response should carry the notice, got %q", opened.Notices.Cache)
	}
	var view mcpserver.ViewResult
	call(t, client, "view", map[string]any{"session": opened.Session, "layout": "active:as-counts"}, &view)
	if view.Notices.Cache != "" {
		t.Errorf("the notice should ride only the response that took it, got %q", view.Notices.Cache)
	}
}

func TestTheCacheNoticeRidesTheFirstResponse(t *testing.T) {
	notice := &onceNotice{}
	env := newTestServer(t, nil, "", "", func(o *testServerOptions) { o.MCP.BeforeCall = notice.beforeCall })
	client := connect(t, env.srv)
	session := openSession(t, client).Session

	notice.set("background cache pruning failed: permission denied; see sdd info")
	var view mcpserver.ViewResult
	call(t, client, "view", map[string]any{"session": session, "layout": "active:as-counts"}, &view)
	if !strings.Contains(view.Notices.Cache, "permission denied") {
		t.Errorf("the next response should carry the failed run's notice, got %q", view.Notices.Cache)
	}

	notice.set("background cache pruning failed: disk full; see sdd info")
	res, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "show", Arguments: map[string]any{"session": session, "ids": []string{"20990101-000000-s-tac-zzz"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(contentText(res), "disk full") {
		t.Errorf("a failed call should carry the notice in its error text, got %q", contentText(res))
	}
}
