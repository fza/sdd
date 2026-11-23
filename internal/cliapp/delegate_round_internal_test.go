package cliapp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/urfave/cli/v3"

	sdd "github.com/networkteam/sdd/pkg/application"
)

// A round's process logs under sdd's state root, keyed by the repository as
// the relevance ledger is.
func TestRoundProcesses_LogUnderTheStateRootPerRepository(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	state := filepath.Join(root, "state")
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	if err := os.MkdirAll(filepath.Join(root, ".sdd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".sdd", "config.yaml"), []byte("repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	processes, err := newRoundProcesses(&cli.Command{})
	if err != nil {
		t.Fatal(err)
	}

	got := processes.Log(sdd.LaunchedRound{Session: "child", Number: 2})
	if want := filepath.Join(state, "sdd", "rounds", "example.test", "cli", "child-round-2.log"); got != want {
		t.Fatalf("the round's log should lie under the state root keyed by the repository, got %s", got)
	}
}
