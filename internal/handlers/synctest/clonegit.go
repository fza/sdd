package synctest

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/networkteam/sdd/internal/model"
)

// CloneGit is a scripted repos.GraphCloneGit: a clone creates .git plus
// Files and reaches no remote. Remotes names the remote URL of the clone at
// a dir, Remote that of every other clone; empty is a local path, which the
// git gate always admits.
type CloneGit struct {
	mu      sync.Mutex
	Clones  int
	Files   map[string]string
	Remotes map[string]string
	Remote  string
}

func (g *CloneGit) CloneGraph(_ context.Context, _ model.RepoID, _ model.CloneSource, dir string, _ []string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Clones++
	err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	if err != nil {
		return err
	}
	for name, content := range g.Files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		err = os.MkdirAll(filepath.Dir(path), 0o755)
		if err != nil {
			return err
		}
		err = os.WriteFile(path, []byte(content), 0o644)
		if err != nil {
			return err
		}
	}
	return nil
}

func (g *CloneGit) SparseCone(context.Context, string) ([]string, error) {
	return model.GraphCloneCone(""), nil
}

func (g *CloneGit) SetSparseCone(context.Context, string, []string) error  { return nil }
func (g *CloneGit) KeepGraphClone(context.Context, string, []string) error { return nil }
func (g *CloneGit) PushGraphClone(context.Context, string) error           { return nil }
func (g *CloneGit) CommitsWaiting(context.Context, string) (bool, error)   { return true, nil }

func (g *CloneGit) RemoteURL(_ context.Context, dir string) (string, error) {
	if url := g.Remotes[dir]; url != "" {
		return url, nil
	}
	if g.Remote != "" {
		return g.Remote, nil
	}
	return "/synctest/remote.git", nil
}

func (g *CloneGit) ExpandURL(_ context.Context, url string) (string, error) { return url, nil }
