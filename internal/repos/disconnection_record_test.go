package repos_test

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

const keptRepo = "repos:\n  - repo_id: github.com/acme/kept\n    clone_url: git@github.com:acme/kept.git\n"

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func recordPath(loc repos.Locations, repoID model.RepoID) string {
	return filepath.Join(loc.DataRoot, "repos", ".disconnected", url.PathEscape(string(repoID)))
}

func TestDisconnectionRecordHoldsOnlyForItsLocations(t *testing.T) {
	root := t.TempDir()
	own := repos.Locations{ConfigPath: filepath.Join(root, "real", "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data")}
	temporary := own
	temporary.ConfigPath = filepath.Join(root, "tmp", "config.yaml")
	otherCache := own
	otherCache.CacheRoot = filepath.Join(root, "other-cache")
	respelled := own
	respelled.ConfigPath = filepath.Join(root, "real", "..", "real", "config.yaml")
	for _, loc := range []repos.Locations{own, temporary} {
		writeFile(t, loc.ConfigPath, keptRepo)
	}
	if err := repos.NewManager(repos.NewRegistry(own), nil).MarkDisconnected("github.com/acme/dropped"); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		loc   repos.Locations
		holds bool
	}{
		{name: "the locations it was written under", loc: own, holds: true},
		{name: "another spelling of the same config path", loc: respelled, holds: true},
		{name: "a temporary config over the same roots", loc: temporary},
		{name: "another cache root", loc: otherCache},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			holds, err := repos.NewRegistry(tc.loc).Disconnected("github.com/acme/dropped")
			if err != nil {
				t.Fatal(err)
			}
			if holds != tc.holds {
				t.Errorf("the record should hold only for the config file and roots it was written under, holds = %v", holds)
			}
		})
	}
}

// A tool pointing XDG_CONFIG_HOME alone at a temporary config, with every XDG
// variable set, reaches this machine's clones and their records.
func TestUnconnectedClonesUnderATemporaryConfigListNothing(t *testing.T) {
	root := t.TempDir()
	own := repos.Locations{ConfigPath: filepath.Join(root, "real", "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data")}
	writeFile(t, own.ConfigPath, keptRepo)
	for _, name := range []string{"dropped", "kept"} {
		if err := os.MkdirAll(filepath.Join(own.DataRoot, "repos", "github.com", "acme", name, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := repos.NewManager(repos.NewRegistry(own), nil).MarkDisconnected("github.com/acme/dropped"); err != nil {
		t.Fatal(err)
	}
	temporary := own
	temporary.ConfigPath = filepath.Join(root, "tmp", "config.yaml")
	writeFile(t, temporary.ConfigPath, "repos:\n  - repo_id: github.com/acme/scratch\n    clone_url: git@github.com:acme/scratch.git\n")
	stale, err := repos.NewRegistry(temporary).UnconnectedClones()
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Errorf("a repos list no record was written under should list no clone for removal, got %+v", stale)
	}
}

func TestDisconnectionRecordFollowsSymlinksAsTheyStoodWhenWritten(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "config-link")
	for _, dir := range []string{"first", "second"} {
		writeFile(t, filepath.Join(root, dir, "config.yaml"), keptRepo)
	}
	if err := os.Symlink(filepath.Join(root, "first"), link); err != nil {
		t.Fatal(err)
	}
	loc := func(configDir string) repos.Locations {
		return repos.Locations{ConfigPath: filepath.Join(configDir, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data")}
	}
	if err := repos.NewManager(repos.NewRegistry(loc(link)), nil).MarkDisconnected("github.com/acme/dropped"); err != nil {
		t.Fatal(err)
	}
	holds, err := repos.NewRegistry(loc(filepath.Join(root, "first"))).Disconnected("github.com/acme/dropped")
	if err != nil {
		t.Fatal(err)
	}
	if !holds {
		t.Error("a record written through a symlink should hold for the config it reached")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "second"), link); err != nil {
		t.Fatal(err)
	}
	holds, err = repos.NewRegistry(loc(link)).Disconnected("github.com/acme/dropped")
	if err != nil {
		t.Fatal(err)
	}
	if holds {
		t.Error("a symlink retargeted since the record was written should reach a config the record does not hold for")
	}
}

func TestAnEmptyDisconnectionRecordHoldsNothing(t *testing.T) {
	for _, content := range []string{"", " \n"} {
		root := t.TempDir()
		loc := repos.Locations{ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data")}
		writeFile(t, loc.ConfigPath, keptRepo)
		if err := os.MkdirAll(filepath.Join(loc.DataRoot, "repos", "github.com", "acme", "dropped", ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, recordPath(loc, "github.com/acme/dropped"), content)
		registry := repos.NewRegistry(loc)
		holds, err := registry.Disconnected("github.com/acme/dropped")
		if err != nil {
			t.Fatal(err)
		}
		if holds {
			t.Errorf("a record naming no locations should hold for none, content %q", content)
		}
		stale, err := registry.UnconnectedClones()
		if err != nil {
			t.Fatal(err)
		}
		if len(stale) != 0 {
			t.Errorf("a clone with an empty record should stay, listed %+v", stale)
		}
	}
}

func TestAnUnreadableDisconnectionRecordFailsThePass(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data")}
	writeFile(t, loc.ConfigPath, keptRepo)
	if err := os.MkdirAll(filepath.Join(loc.DataRoot, "repos", "github.com", "acme", "dropped", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	record := recordPath(loc, "github.com/acme/dropped")
	writeFile(t, record, "not a record")
	stale, err := repos.NewRegistry(loc).UnconnectedClones()
	if err == nil {
		t.Fatalf("a record sdd cannot read should fail the pass, listed %+v", stale)
	}
	if !strings.Contains(err.Error(), record) || !strings.Contains(err.Error(), "delete") {
		t.Errorf("the failure should name the record and how to clear it, got %v", err)
	}
}
