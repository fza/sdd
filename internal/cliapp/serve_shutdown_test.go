package cliapp_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/test"
)

// A stopped serve exits within Claude Code's shutdown window while the title
// backfill a session start began still waits on the model: the stop ends the
// backfill instead of waiting it out.
func TestServeExitsWithinTheWindowWhileTheBackfillWaitsOnTheModel(t *testing.T) {
	f := newAppFixture(t)
	llm := test.NewOpenAIServer(t, test.OpenAIBehaviour{Answer: `{"title": "Late title"}`, Delay: time.Minute})
	embeddings := httptest.NewServer(&fakeOllama{})
	t.Cleanup(embeddings.Close)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\nverify:\n  mode: off\n")
	f.write(t, ".sdd/config.local.yaml", "participant: Local Author\nllm:\n  provider: openai\n  model: fake\n  endpoint: "+llm.URL+"\n  api_keys:\n    openai: op-notarealkeyaaaaaaaaaa\n  retry:\n    max_attempts: 1\n"+
		"embedding:\n  provider: ollama\n  model: fake-model\n  ollama_endpoint: "+embeddings.URL+"\n"+titlesBackfillOn)
	f.write(t, "decisions/2026/11/01-100000-s-tac-unt.md", untitledEntryContent)
	f.write(t, ".gitignore", "config/\ncache/\nstate/\n.sdd/config.local.yaml\n.sdd/tmp/\n")
	initRepo(t, f.root)
	gitIn(t, f.root, "add", ".gitignore", ".sdd/config.yaml", "decisions")
	gitIn(t, f.root, "commit", "-q", "-m", "graph")

	serverInput, clientOutput := io.Pipe()
	clientInput, serverOutput := io.Pipe()
	t.Cleanup(func() {
		for _, pipe := range []io.Closer{serverInput, clientOutput, clientInput, serverOutput} {
			_ = pipe.Close()
		}
	})
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	var stderr bytes.Buffer
	returned := make(chan error, 1)
	go func() {
		returned <- f.command(serverInput, serverOutput, &stderr).Run(ctx, []string{"sdd", "serve", "--transport", "stdio"})
	}()
	client, err := mcp.NewClient(&mcp.Implementation{Name: "claude-code", Version: "test"}, nil).Connect(t.Context(), &mcp.IOTransport{Reader: clientInput, Writer: clientOutput}, nil)
	if err != nil {
		t.Fatalf("connecting to sdd serve: %v\n%s", err, stderr.String())
	}
	if _, served, failed := callTool(t, client, "start_session", map[string]any{}); failed {
		t.Fatalf("start_session: %s", served)
	}
	deadline := time.Now().Add(10 * time.Second)
	for len(llm.Requests()) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the session start should have begun a title backfill\n%s", stderr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}

	stopped := time.Now()
	stop()
	select {
	case err = <-returned:
	case <-time.After(10 * time.Second):
		t.Fatal("serve should return after the stop")
	}
	if took := time.Since(stopped); took > time.Second {
		t.Errorf("serve should exit within the shutdown window, took %s: %v\n%s", took, err, stderr.String())
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the backfill should end with the stop, leaving nothing for the window to cut, got %v", err)
	}
	if gitIn(t, f.root, "status", "--porcelain", "decisions") != "" {
		t.Error("the stopped backfill should leave no title written and uncommitted")
	}
}
