package cliapp_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Where titles.backfill is on, sdd serve returns only once the title backfill
// a session start began has committed, so no write outlives the process;
// elsewhere a session start titles nothing.
func TestServeBackfillsTitlesAtSessionStartOnlyWhereSwitchedOn(t *testing.T) {
	for _, tc := range []struct {
		name     string
		switched string
		titled   bool
	}{
		{name: "switched on", switched: titlesBackfillOn, titled: true},
		{name: "not switched on"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFillFixture(t)
			f.write(t, ".sdd/config.local.yaml", "participant: Local Author\n"+tc.switched)

			f.serve(t, func(client *mcp.ClientSession) {
				openServedSession(t, client)
			})

			subject := headSubject(t, f.root)
			if strings.HasPrefix(subject, "sdd: summarize") != tc.titled {
				t.Errorf("the backfill's commit should be in place when serve returns only with the switch on, head is %q", subject)
			}
			if strings.Contains(f.read(t, appEntryRel), "title:") != tc.titled {
				t.Error("the entry should be titled only with the switch on")
			}
		})
	}
}

// A stopped serve exits at once while the title backfill a session start
// began still waits on the model: the stop ends the backfill instead of
// waiting it out.
func TestServeExitsAtOnceWhileTheBackfillWaitsOnTheModel(t *testing.T) {
	asked := make(chan struct{}, 1)
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			_, _ = w.Write([]byte(`{"models":[]}`))
			return
		}
		select {
		case asked <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
		case <-time.After(time.Minute):
		}
	}))
	t.Cleanup(llm.Close)
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nllm:\n  provider: ollama\n  model: m\n  ollama_endpoint: "+llm.URL+"\n")
	f.write(t, ".sdd/config.local.yaml", "participant: Local Author\n"+titlesBackfillOn)
	runGit(t, f.root, "init", "-q", "-b", "main")
	runGit(t, f.root, "config", "user.email", "test@example.com")
	runGit(t, f.root, "config", "user.name", "Test")
	runGit(t, f.root, "config", "commit.gpgsign", "false")
	runGit(t, f.root, "add", ".sdd/config.yaml", "decisions")
	runGit(t, f.root, "commit", "-q", "-m", "graph")

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
	client, err := mcp.NewClient(&mcp.Implementation{Name: "cli-test", Version: "test"}, nil).Connect(t.Context(), &mcp.IOTransport{Reader: clientInput, Writer: clientOutput}, nil)
	if err != nil {
		t.Fatalf("connecting to sdd serve: %v\n%s", err, stderr.String())
	}
	openServedSession(t, client)
	select {
	case <-asked:
	case <-time.After(10 * time.Second):
		t.Fatalf("the session start should have begun a title backfill\n%s", stderr.String())
	}

	stopped := time.Now()
	stop()
	select {
	case err = <-returned:
	case <-time.After(10 * time.Second):
		t.Fatal("serve should return after the stop")
	}
	if took := time.Since(stopped); took > time.Second {
		t.Errorf("serve should exit at once, took %s: %v\n%s", took, err, stderr.String())
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("serve should end with the stop, got %v", err)
	}
	if runGit(t, f.root, "status", "--porcelain", "decisions") != "" {
		t.Error("the stopped backfill should leave no title written and uncommitted")
	}
}
