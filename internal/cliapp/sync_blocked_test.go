package cliapp_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/cliapp"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/syncstate"
)

// cloneOf makes the graph clone of the code repository's graph_repo, as its
// first command does, and returns the clone's graph directory.
func cloneOf(t *testing.T, code string) string {
	t.Helper()
	runApp(t, code, "view", "--layout", "layer(tac):as-list")
	return filepath.Join(cloneDir(t), ".sdd", "graph")
}

// pauseOnConflict records a conflict with source in the clone's sync state,
// as a round meeting one would.
func pauseOnConflict(t *testing.T, graphDir, source, destination string) {
	t.Helper()
	_, state, err := syncstate.Open(t.Context(), graphDir)
	if err != nil {
		t.Fatal(err)
	}
	err = state.Update(func(*model.SyncBlocked) *model.SyncBlocked {
		return &model.SyncBlocked{
			Kind: model.SyncBlockedConflict, Since: time.Now(), Upstream: "origin/main", Paths: []string{".sdd/graph/shared.md"},
			Source: source, Destination: destination,
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBlockedSyncBannerPrintsOncePerCommand(t *testing.T) {
	code, remote := graphRepoCode(t)
	cloneOf(t, code)
	err := os.Rename(remote.Bare, remote.Bare+".gone")
	if err != nil {
		t.Fatal(err)
	}

	for range 2 {
		out, err := runAppErr(t, code, "view", "--layout", "layer(tac):as-list")
		if err != nil {
			t.Fatalf("view should succeed: %v\n%s", err, out)
		}
		if got := strings.Count(out, "sync blocked since"); got != 1 {
			t.Errorf("each command should print the failing fetch once, got %d in %q", got, out)
		}
		if strings.Contains(out, "▲") {
			t.Errorf("the banner should be a plain line, not a warning, got %q", out)
		}
	}
}

const (
	conflictSource      = "1111111111111111111111111111111111111111"
	conflictDestination = "2222222222222222222222222222222222222222"
)

func TestInfoShowsTheConflictRecord(t *testing.T) {
	code, _ := graphRepoCode(t)
	pauseOnConflict(t, cloneOf(t, code), conflictSource, conflictDestination)

	out, err := runAppErr(t, code, "info")
	if err != nil {
		t.Fatalf("info should succeed: %v\n%s", err, out)
	}
	for _, want := range []string{"Sync blocked: conflict since", "Upstream: origin/", "Source: " + conflictSource, "Destination: " + conflictDestination, `Paths: ".sdd/graph/shared.md"`, "git merge origin/"} {
		if !strings.Contains(out, want) {
			t.Errorf("info should show the record and its fix, missing %q in %q", want, out)
		}
	}
}

func TestInfoJSONCarriesTheConflictRecord(t *testing.T) {
	code, _ := graphRepoCode(t)
	pauseOnConflict(t, cloneOf(t, code), conflictSource, conflictDestination)
	t.Chdir(code)
	var stdout, stderr bytes.Buffer
	command := cliapp.New(cliapp.Options{Version: "test", Reader: strings.NewReader(""), Writer: &stdout, ErrWriter: &stderr})
	if err := command.Run(t.Context(), []string{"sdd", "info", "--format", "json"}); err != nil {
		t.Fatalf("info --format json should succeed: %v\n%s", err, stderr.String())
	}
	var info struct {
		SyncBlocked *struct {
			Kind        string   `json:"kind"`
			Since       string   `json:"since"`
			Source      string   `json:"source"`
			Destination string   `json:"destination"`
			Paths       []string `json:"paths"`
		} `json:"sync_blocked"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		t.Fatalf("info --format json should print one JSON object: %v\n%s", err, stdout.String())
	}
	block := info.SyncBlocked
	if block == nil || block.Kind != "conflict" || block.Since == "" || block.Source != conflictSource || block.Destination != conflictDestination ||
		len(block.Paths) != 1 || block.Paths[0] != ".sdd/graph/shared.md" {
		t.Errorf("the JSON form should carry the conflict record, got %s", stdout.String())
	}
}

func TestExplicitSyncRunsWhilePaused(t *testing.T) {
	code, remote := graphRepoCode(t)
	graphDir := cloneOf(t, code)
	later := remote.Commit(t, "sdd: capture later", map[string]string{".sdd/graph/later.md": "later\n"})
	pauseOnConflict(t, graphDir, later, conflictDestination)

	// The tracking ref tells: reading an object of a partial clone may load it.
	tracked := func() string {
		return strings.TrimSpace(gitIn(t, cloneDir(t), "rev-parse", "refs/remotes/origin/main"))
	}
	before := tracked()
	runApp(t, code, "view", "--layout", "layer(tac):as-list")
	if tracked() != before {
		t.Fatal("a paused check should not fetch")
	}
	out, err := runAppErr(t, code, "sync")
	if err != nil {
		t.Fatalf("an explicit sync should run while paused: %v\n%s", err, out)
	}
	if tracked() != later {
		t.Error("an explicit sync should fetch while paused")
	}
	blocked, err := syncstate.ReadBlocked(t.Context(), graphDir)
	if err != nil {
		t.Fatal(err)
	}
	if blocked != nil {
		t.Errorf("an explicit round merging the conflict's source should lift the pause, got %+v", blocked)
	}
}

// fetched reports whether the repository at root holds commit.
func fetched(root, commit string) bool {
	cmd := exec.Command("git", "cat-file", "-e", commit+"^{commit}")
	cmd.Dir = root
	return cmd.Run() == nil
}

// Whatever sync.mode says, a graph directory in no repository syncs nothing
// and reports no other repository's state as its own.
func TestMissingGraphDirectoryNeverSyncsAnotherRepository(t *testing.T) {
	for _, mode := range []string{"manual", "auto"} {
		t.Run(mode, func(t *testing.T) {
			root := canonicalTemp(t)
			if err := os.MkdirAll(filepath.Join(root, ".sdd"), 0o755); err != nil {
				t.Fatal(err)
			}
			initRepo(t, root)
			writeFile(t, filepath.Join(root, ".sdd", "config.yaml"), "graph_dir: ../missing-graph-dir\nrepo_id: example.test/missing\nlanguage: en\n")
			writeFile(t, filepath.Join(root, ".sdd", "config.local.yaml"), "participant: Ada\nsync:\n  mode: "+mode+"\n")

			out, _ := runAppErr(t, root, "view", "--layout", "layer(tac):as-list")
			if strings.Contains(out, "no remote configured") {
				t.Errorf("sync should not report the enclosing repository's state as the graph's, got %q", out)
			}
			if strings.Contains(out, "sync blocked") {
				t.Errorf("a graph inside the code repository has no automatic sync to report blocked, got %q", out)
			}
		})
	}
}

func TestSyncIntervalIsPerPersonAndFloored(t *testing.T) {
	root := canonicalTemp(t)
	if err := os.MkdirAll(filepath.Join(root, ".sdd", "graph"), 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, root)
	writeFile(t, filepath.Join(root, ".sdd", "config.yaml"), "graph_dir: .sdd/graph\nrepo_id: example.test/interval\nlanguage: en\nsync:\n  interval: 5m\n")
	writeFile(t, filepath.Join(root, ".sdd", "config.local.yaml"), "participant: Ada\n")

	out, err := runAppErr(t, root, "view", "--layout", "layer(tac):as-list")
	if err != nil {
		t.Fatalf("a committed interval should not stop a command: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ignoring config setting") || !strings.Contains(out, "sync.interval") || !strings.Contains(out, "config.yaml") {
		t.Errorf("a committed interval should be reported naming file and key, got %q", out)
	}
	for _, value := range []string{"30s", "often", "2m"} {
		out, err := runAppErr(t, root, "config", "set", "--local", "sync.interval", value)
		if err == nil || !strings.Contains(err.Error(), "no longer used") {
			t.Errorf("config set should refuse the retired sync.interval whatever its value, got %v\n%s", err, out)
		}
	}
}
