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

func TestTheCacheNoticeRidesOneResponse(t *testing.T) {
	notice := &onceNotice{notice: "background cache pruning failed at 2026-11-01T12:00:00Z: permission denied; see sdd info"}
	env := newTestServer(t, nil, "", "", func(o *testServerOptions) { o.MCP.CacheNotice = notice.take })
	client := connect(t, env.srv)
	opened := openSession(t, client)
	if !strings.Contains(opened.CacheNotice, "permission denied") {
		t.Errorf("the first response should carry the notice, got %q", opened.CacheNotice)
	}
	var view mcpserver.ViewResult
	call(t, client, "view", map[string]any{"session": opened.Session, "layout": "active:as-counts"}, &view)
	if view.CacheNotice != "" {
		t.Errorf("the notice should ride only the response that took it, got %q", view.CacheNotice)
	}
}

func TestTheCacheNoticeRidesTheFirstResponse(t *testing.T) {
	notice := &onceNotice{}
	env := newTestServer(t, nil, "", "", func(o *testServerOptions) { o.MCP.CacheNotice = notice.take })
	client := connect(t, env.srv)
	session := openSession(t, client).Session

	notice.mu.Lock()
	notice.notice = "background cache pruning failed: permission denied; see sdd info"
	notice.mu.Unlock()
	var view mcpserver.ViewResult
	call(t, client, "view", map[string]any{"session": session, "layout": "active:as-counts"}, &view)
	if !strings.Contains(view.CacheNotice, "permission denied") {
		t.Errorf("the next response should carry the failed run's notice, got %q", view.CacheNotice)
	}

	notice.mu.Lock()
	notice.notice = "background cache pruning failed: disk full; see sdd info"
	notice.mu.Unlock()
	res, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "show", Arguments: map[string]any{"session": session, "ids": []string{"20990101-000000-s-tac-zzz"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(toolText(res), "disk full") {
		t.Errorf("a failed call should carry the notice in its error text, got %q", toolText(res))
	}
}
