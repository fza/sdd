package mcpapp_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// testGraphSync serves a scripted blocked-sync record; a block appearing
// during a call (blockOnRound) stands for what the call's own round recorded.
type testGraphSync struct {
	mu           sync.Mutex
	blocked      *model.SyncBlocked
	blockOnRound *model.SyncBlocked
}

func (s *testGraphSync) SyncGraph(context.Context, string, sdd.SyncTrigger) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blockOnRound != nil {
		s.blocked, s.blockOnRound = s.blockOnRound, nil
	}
	return nil
}

func (s *testGraphSync) SyncNotice(_ context.Context, _, session string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blocked == nil {
		return "", nil
	}
	claimed := s.blocked.Claim(session, time.Now())
	s.blocked = &claimed
	return claimed.NoticeFor(session), nil
}

func (s *testGraphSync) ReleaseSyncOwner(_ context.Context, _, session string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blocked == nil {
		return "", nil
	}
	released := s.blocked.Release(session)
	s.blocked = &released
	return released.Banner(), nil
}

func (s *testGraphSync) SyncInterval(context.Context, string) (time.Duration, bool, error) {
	return 0, false, nil
}

func (s *testGraphSync) SyncBlocked(context.Context, string) (*sdd.SyncBlocked, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blocked == nil {
		return nil, nil
	}
	return &sdd.SyncBlocked{
		Kind: string(s.blocked.Kind), Since: s.blocked.Since, Upstream: s.blocked.Upstream, Source: s.blocked.Source,
		Destination: s.blocked.Destination, Paths: s.blocked.Paths, Owner: s.blocked.Owner,
	}, nil
}

func conflictBlock() *model.SyncBlocked {
	return &model.SyncBlocked{
		Kind: model.SyncBlockedConflict, Since: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), Upstream: "origin/main",
		Source: "0123456789abcdef", Destination: "fedcba9876543210", Paths: []string{".sdd/graph/a.md"},
	}
}

func TestEveryResponseCarriesTheSyncNotice(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	owner := connect(t, env.srv)
	other := connect(t, env.srv)
	ownerSession := openSession(t, owner).Session
	otherSession := openSession(t, other).Session
	env.sync.blockOnRound = conflictBlock()

	var view mcpserver.ViewResult
	call(t, owner, "view", map[string]any{"session": ownerSession, "layout": "active:as-counts"}, &view)
	if !strings.Contains(view.SyncNotice, "sync blocked since") || !strings.Contains(view.SyncNotice, "git merge origin/main") {
		t.Errorf("the first call after the block should carry the banner and, as owner, the steps, got %q", view.SyncNotice)
	}
	var registry mcpserver.RegistryResult
	call(t, other, "registry", map[string]any{"session": otherSession}, &registry)
	if !strings.HasPrefix(registry.SyncNotice, "sync blocked since") || strings.Contains(registry.SyncNotice, "git merge") {
		t.Errorf("another agent should get the banner and no steps, got %q", registry.SyncNotice)
	}
	var serve mcpserver.ServeResult
	call(t, owner, "resume_session", map[string]any{"session": ownerSession}, &serve)
	if !strings.Contains(serve.SyncNotice, "git merge origin/main") {
		t.Errorf("the owner's served steps should keep carrying the fix, got %q", serve.SyncNotice)
	}
	var info mcpserver.InfoResult
	call(t, other, "info", map[string]any{"session": otherSession}, &info)
	got := info.SyncBlocked
	if got == nil || got.Kind != "conflict" || got.Source != "0123456789abcdef" || got.Destination != "fedcba9876543210" || got.Upstream != "origin/main" || len(got.Paths) != 1 || got.Since == "" {
		t.Errorf("info should show the whole record, got %+v", got)
	}

	env.sync.mu.Lock()
	env.sync.blocked = nil
	env.sync.mu.Unlock()
	call(t, owner, "view", map[string]any{"session": ownerSession, "layout": "active:as-counts"}, &view)
	if view.SyncNotice != "" {
		t.Errorf("no notice should ride once sync proceeds, got %q", view.SyncNotice)
	}
}

func TestFailedCallsAndTeardownsCarryTheSyncNotice(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	owner := connect(t, env.srv)
	other := connect(t, env.srv)
	ownerSession := openSession(t, owner).Session
	otherSession := openSession(t, other).Session
	env.sync.blocked = conflictBlock()

	res, err := owner.CallTool(t.Context(), &mcp.CallToolParams{Name: "show", Arguments: map[string]any{"session": ownerSession, "ids": []string{"20990101-000000-s-tac-zzz"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(toolText(res), "sync blocked since") {
		t.Errorf("a failed call should still carry the banner, got %q", toolText(res))
	}
	var abandoned mcpserver.AbandonResult
	call(t, owner, "abandon", map[string]any{"session": ownerSession, "reason": "done here"}, &abandoned)
	if !strings.HasPrefix(abandoned.SyncNotice, "sync blocked since") || strings.Contains(abandoned.SyncNotice, "git merge") {
		t.Errorf("a teardown by handle should carry the banner without the steps, got %q", abandoned.SyncNotice)
	}
	var view mcpserver.ViewResult
	call(t, other, "view", map[string]any{"session": otherSession, "layout": "active:as-counts"}, &view)
	if !strings.Contains(view.SyncNotice, "git merge origin/main") {
		t.Errorf("once the owner's session is torn down the next caller should own the fix, got %q", view.SyncNotice)
	}
}

func toolText(res *mcp.CallToolResult) string {
	var text []string
	for _, content := range res.Content {
		if tc, ok := content.(*mcp.TextContent); ok {
			text = append(text, tc.Text)
		}
	}
	return strings.Join(text, "\n")
}

func TestSyncNoticeIsPlainContent(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session
	env.sync.blocked = conflictBlock()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "view", Arguments: map[string]any{"session": session, "layout": "active:as-counts"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Error("a blocked sync should never turn a response into an error")
	}
	var structured map[string]any
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &structured); err != nil {
		t.Fatal(err)
	}
	if _, ok := structured["sync_notice"]; !ok {
		t.Errorf("the notice should ride the structured result as sync_notice, got %v", structured)
	}
}

func TestEveryToolSchemaListsTheSyncNotice(t *testing.T) {
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
		if !strings.Contains(string(encoded), `"sync_notice"`) {
			t.Errorf("tool %s should list sync_notice in its output schema", tool.Name)
		}
	}
}

func TestConcludingTheOwnerHandsTheFixOn(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	owner := connect(t, env.srv)
	other := connect(t, env.srv)
	shell := openSession(t, owner)
	otherSession := openSession(t, other).Session
	env.sync.blocked = conflictBlock()

	var view mcpserver.ViewResult
	call(t, owner, "view", map[string]any{"session": shell.Session, "layout": "active:as-counts"}, &view)
	var concluded mcpserver.ServeResult
	call(t, owner, "next", map[string]any{"session": shell.Session, "instance": shell.Instance, "report": map[string]any{
		"chooser": "junction", "choice": "conclude", "userWords": "done",
	}}, &concluded)
	if !strings.HasPrefix(concluded.SyncNotice, "sync blocked since") || strings.Contains(concluded.SyncNotice, "git merge") {
		t.Errorf("the concluding call should carry the banner without the steps, got %q", concluded.SyncNotice)
	}
	call(t, other, "view", map[string]any{"session": otherSession, "layout": "active:as-counts"}, &view)
	if !strings.Contains(view.SyncNotice, "git merge origin/main") {
		t.Errorf("once the owner concluded the next caller should own the fix, got %q", view.SyncNotice)
	}
}
