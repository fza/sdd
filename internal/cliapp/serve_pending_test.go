package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/test"
)

// sdd serve answers the MCP handshake before the first clone of the graph
// repository, and each call with the clone's state and the fix, rather than
// failing to start once the clone fails.
func TestServeAnswersBeforeTheFirstGraphClone(t *testing.T) {
	const graphRepoID = model.RepoID("example.test/acme/unreachable-sdd")
	remote := test.NewGraphRemote(t, graphRepoID)
	if err := os.RemoveAll(remote.Bare); err != nil {
		t.Fatal(err)
	}
	code := filepath.Join(canonicalTemp(t), "code")
	if err := os.MkdirAll(code, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, code)
	writeFile(t, filepath.Join(code, ".sdd", "config.yaml"), "graph_repo: "+string(graphRepoID)+"\nskill_scope: project\n")
	writeFile(t, filepath.Join(code, ".gitignore"), ".sdd/config.local.yaml\nxdg-*/\n")
	gitIn(t, code, "add", ".sdd", ".gitignore")
	gitIn(t, code, "commit", "-q", "-m", "point at the graph repository")

	t.Chdir(code)
	fixture := &appFixture{root: code}
	fixture.serve(t, func(client *mcp.ClientSession) {
		_, served, failed := callTool(t, client, "start_session", map[string]any{})
		if !failed || !strings.Contains(served, "sdd repo sync "+string(graphRepoID)) {
			t.Errorf("a call before the graph clone stands should fail naming the fix:\n%s", served)
		}
	})
}
