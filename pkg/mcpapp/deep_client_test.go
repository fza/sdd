package mcpapp_test

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// connectNamed connects an MCP client that reports name in its client info.
func connectNamed(t *testing.T, srv *mcpserver.Server, name string) *mcp.ClientSession {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	_, err := srv.Connect(t.Context(), serverTransport)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: name, Version: "test"}, nil).Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cs.Close(); err != nil {
			t.Errorf("closing MCP client: %v", err)
		}
	})
	return cs
}

func verifyServer(t *testing.T, policy sdd.VerifierPolicy) *mcpserver.Server {
	t.Helper()
	world := proctest.NewWorld(t, proctest.WithVerifier(policy, 200000, 4096))
	srv, err := mcpserver.New(mcpserver.Options{Application: world.App, SearchSyncMode: sdd.SearchSyncAll, LocalIdentity: world.Identity, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func callOutcome(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	return contentText(res), res.IsError
}

// Deep verification runs only in Claude Code: under a deep policy a Codex
// client cannot start a session, and the refusal names the setting.
func TestStartSession_DeepRefusesACodexClient(t *testing.T) {
	cases := []struct {
		name, client string
		policy       sdd.VerifierPolicy
		refusal      string
	}{
		{"codex under deep", "codex-mcp-client", sdd.VerifierPolicy{Mode: "deep"}, "verify.mode deep needs Claude Code"},
		{"codex under a deep kind", "codex-mcp-client", sdd.VerifierPolicy{Mode: "prose", Kinds: map[string]string{"plan": "deep"}}, "verify.kinds.plan deep needs Claude Code"},
		{"codex under deep with one kind lower", "codex-mcp-client", sdd.VerifierPolicy{Mode: "deep", Kinds: map[string]string{"plan": "prose"}}, "verify.mode deep needs Claude Code"},
		{"codex under prose", "codex-mcp-client", sdd.VerifierPolicy{Mode: "prose"}, ""},
		{"claude code under deep", "claude-code", sdd.VerifierPolicy{Mode: "deep"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cs := connectNamed(t, verifyServer(t, tc.policy), tc.client)

			text, isError := callOutcome(t, cs, "start_session", map[string]any{})

			if tc.refusal == "" && isError {
				t.Fatalf("the session should start, got %s", text)
			}
			if tc.refusal != "" && (!isError || !strings.Contains(text, tc.refusal)) {
				t.Fatalf("the session should be refused naming the setting, got %s", text)
			}
		})
	}
}

// A Codex resume is refused before the session's hooks are asked about: the
// refusal, never a hook confirmation, answers it.
func TestResumeSession_RefusesACodexClientBeforeItsHooks(t *testing.T) {
	hooks := &proctest.ScriptedHooks{Configured: []sdd.Hook{{HookConfig: model.HookConfig{Event: "capture.assemble.start", Command: "notify"}}}}
	world := proctest.NewWorld(t, proctest.WithVerifier(sdd.VerifierPolicy{Mode: "deep"}, 200000, 4096), proctest.WithHooks(hooks))
	srv, err := mcpserver.New(mcpserver.Options{Application: world.App, SearchSyncMode: sdd.SearchSyncAll, LocalIdentity: world.Identity, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	var serve mcpserver.ServeResult
	call(t, connectNamed(t, srv, "claude-code"), "start_session", map[string]any{}, &serve)
	hooks.Configured[0].State = model.HookPending

	text, isError := callOutcome(t, connectNamed(t, srv, "codex-mcp-client"), "resume_session", map[string]any{"session": serve.Session})

	if !isError || !strings.Contains(text, "verify.mode deep needs Claude Code") {
		t.Fatalf("the resume should be refused before any hook is asked about, got %s", text)
	}
}

// A session a Claude client opened under deep cannot be resumed from Codex.
func TestResumeSession_DeepRefusesACodexClient(t *testing.T) {
	srv := verifyServer(t, sdd.VerifierPolicy{Mode: "deep"})
	var serve mcpserver.ServeResult
	call(t, connectNamed(t, srv, "claude-code"), "start_session", map[string]any{}, &serve)

	text, isError := callOutcome(t, connectNamed(t, srv, "codex-mcp-client"), "resume_session", map[string]any{"session": serve.Session})

	if !isError || !strings.Contains(text, "verify.mode deep needs Claude Code") {
		t.Fatalf("the resume should be refused naming the setting, got %s", text)
	}
}
