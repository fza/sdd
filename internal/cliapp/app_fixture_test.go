package cliapp_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/networkteam/sdd/internal/cliapp"
	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/internal/testsupport"
	"github.com/urfave/cli/v3"
)

const (
	appVersion = "1.2.3-test"
	appEntryID = "20260914-020000-s-tac-cli"
	appSummary = "Configured CLI reads use the selected graph."
	appBody    = "The command renders this entry through the configured graph directory."
)

// appFixture runs commands in a project of its own. releases stands in for
// GitHub, so no command reaches it.
type appFixture struct {
	root string
	// client is the name the serve's MCP client reports; cli-test when empty.
	client   string
	releases *testsupport.GitleaksReleases
}

func newAppFixture(t *testing.T) *appFixture {
	t.Helper()
	f := &appFixture{root: t.TempDir(), releases: testsupport.NewGitleaksReleases(t)}
	// GitHub always names a latest release; with none newer than the
	// built-in file a scheduled pass only records its check.
	builtin, err := secrets.BuiltinRuleFile()
	if err != nil {
		t.Fatal(err)
	}
	f.releases.Publish(builtin.Tag, builtin.Data)
	t.Chdir(f.root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(f.root, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(f.root, "cache"))
	t.Setenv("XDG_DATA_HOME", dataHome(f.root))
	t.Setenv("XDG_STATE_HOME", filepath.Join(f.root, "state"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(f.root, "data"))
	// A request that still leaves for a real host fails at a closed port.
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
	f.write(t, "config/sdd/config.yaml", "participant: Global Author\n")
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\n")
	f.write(t, ".sdd/config.local.yaml", "participant: Local Author\n")
	f.write(t, "decisions/2026/09/14-020000-s-tac-cli.md", "---\ntype: signal\nkind: gap\nlayer: tactical\nconfidence: high\nparticipants: [Local Author]\nsummary: "+appSummary+"\n---\n\n"+appBody+"\n")
	return f
}

// dataHome is the XDG data dir of a test working in dir: beside dir, so the
// sdd-owned clones under it stay out of the repository the test inspects.
func dataHome(dir string) string {
	return filepath.Join(filepath.Dir(dir), filepath.Base(dir)+"-xdg-data")
}

func (f *appFixture) write(t *testing.T, name, content string) {
	t.Helper()
	filename := filepath.Join(f.root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

type appResult struct {
	stdout, stderr string
	err            error
}

func (f *appFixture) run(t *testing.T, args ...string) appResult {
	t.Helper()
	var stdout, stderr bytes.Buffer
	command := f.command(strings.NewReader(""), &stdout, &stderr)
	err := command.Run(t.Context(), append([]string{"sdd"}, args...))
	return appResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

func (f *appFixture) command(reader io.Reader, writer, errWriter io.Writer) *cli.Command {
	return f.commandWith(reader, writer, errWriter, nil)
}

// commandWith builds the CLI with launch standing in for the detached cache
// pruning process; nil launches none.
func (f *appFixture) commandWith(reader io.Reader, writer, errWriter io.Writer, launch func(dir, logPath string) error) *cli.Command {
	return cliapp.New(cliapp.Options{
		Version: appVersion, Reader: reader, Writer: writer, ErrWriter: errWriter,
		Gitleaks: f.gitleaks(), LaunchCachePrune: launch,
	})
}

func (f *appFixture) gitleaks() *secrets.GitHub {
	return f.releases.GitHub()
}

func (r appResult) exitCode() int {
	if r.err == nil {
		return 0
	}
	var exit cli.ExitCoder
	if errors.As(r.err, &exit) {
		return exit.ExitCode()
	}
	return 1
}

func assertContainsOutput(t *testing.T, stream, output string, expected ...string) {
	t.Helper()
	for _, text := range expected {
		if !strings.Contains(output, text) {
			t.Errorf("%s omitted %q:\n%s", stream, text, output)
		}
	}
}

func (f *appFixture) serve(t *testing.T, check func(*mcp.ClientSession)) {
	t.Helper()
	f.serveRun(t, nil, nil, check)
}

// serveArgs is serve with extra serve flags.
func (f *appFixture) serveArgs(t *testing.T, flags []string, check func(*mcp.ClientSession)) {
	t.Helper()
	f.serveRun(t, flags, nil, check)
}

// serveWith runs sdd serve with launch as its cache pruning launcher (see
// commandWith).
func (f *appFixture) serveWith(t *testing.T, launch func(dir, logPath string) error, check func(*mcp.ClientSession)) {
	t.Helper()
	f.serveRun(t, nil, launch, check)
}

// serveRun runs sdd serve with extra serve flags and launch as its cache
// pruning launcher.
func (f *appFixture) serveRun(t *testing.T, flags []string, launch func(dir, logPath string) error, check func(*mcp.ClientSession)) {
	t.Helper()
	serverInput, clientOutput := io.Pipe()
	clientInput, serverOutput := io.Pipe()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	var stderr bytes.Buffer
	command := f.commandWith(serverInput, serverOutput, &stderr, launch)
	done := make(chan struct{})
	var runErr error
	go func() {
		runErr = command.Run(ctx, append([]string{"sdd", "serve", "--transport", "stdio"}, flags...))
		close(done)
	}()
	defer func() {
		cancel()
		for _, pipe := range []io.Closer{serverInput, clientOutput, clientInput, serverOutput} {
			if err := pipe.Close(); err != nil {
				t.Error(err)
			}
		}
		select {
		case <-done:
			if t.Failed() {
				t.Logf("sdd serve: %v\n%s", runErr, stderr.String())
			}
		case <-time.After(10 * time.Second):
			t.Error("sdd serve did not stop after closing its streams")
		}
	}()

	name := f.client
	if name == "" {
		name = "cli-test"
	}
	var client *mcp.ClientSession
	var err error
	connected := make(chan struct{})
	go func() {
		client, err = mcp.NewClient(&mcp.Implementation{Name: name, Version: "test"}, nil).Connect(ctx, &mcp.IOTransport{Reader: clientInput, Writer: clientOutput}, nil)
		close(connected)
	}()
	select {
	case <-connected:
	case <-done:
		t.Fatalf("sdd serve ended before answering the MCP handshake: %v\n%s", runErr, stderr.String())
	}
	if err != nil {
		t.Fatalf("initialize CLI MCP: %v", err)
	}
	check(client)
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		if runErr != nil {
			t.Fatalf("CLI Run after MCP disconnect: %v\n%s", runErr, stderr.String())
		}
	case <-ctx.Done():
		t.Fatal("CLI Run did not return after MCP disconnect")
	}
}
