package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// GraphRemote is a graph repository on disk standing in for one on a host:
// git rewrites the https URL sdd derives from its id to a bare repository,
// and the ssh URL to a path that does not exist, unless ssh is made to work.
type GraphRemote struct {
	RepoID    model.RepoID
	Bare      string
	home      string
	gitconfig string
}

// IsolatedHome points HOME, the XDG directories and git's global config at a
// temporary directory, so nothing reaches the developer's own, and commits
// stay unsigned whatever the developer's setup. It returns that directory.
func IsolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, xdg := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME"} {
		t.Setenv(xdg, filepath.Join(home, strings.ToLower(strings.TrimPrefix(xdg, "XDG_"))))
	}
	gitconfig := filepath.Join(home, "gitconfig")
	err := os.WriteFile(gitconfig, []byte("[user]\n\temail = test@example.com\n\tname = Test\n[commit]\n\tgpgsign = false\n[tag]\n\tgpgsign = false\n[init]\n\tdefaultBranch = main\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", gitconfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	return home
}

// NewGraphRemote creates an empty bare repository for repoID under an
// isolated home.
func NewGraphRemote(t *testing.T, repoID model.RepoID) *GraphRemote {
	t.Helper()
	return newGraphRemote(t, IsolatedHome(t), repoID)
}

// NewGraphRemoteBeside creates an empty bare repository for repoID in the
// isolated home of other, both reachable at once.
func NewGraphRemoteBeside(t *testing.T, other *GraphRemote, repoID model.RepoID) *GraphRemote {
	t.Helper()
	return newGraphRemote(t, other.home, repoID)
}

func newGraphRemote(t *testing.T, home string, repoID model.RepoID) *GraphRemote {
	t.Helper()
	bare := filepath.Join(home, "remotes", filepath.FromSlash(string(repoID))+".git")
	Git(t, home, "init", "-q", "--bare", "-b", "main", bare)
	Git(t, bare, "config", "uploadpack.allowFilter", "true")
	Git(t, bare, "config", "uploadpack.allowAnySHA1InWant", "true")
	remote := &GraphRemote{RepoID: repoID, Bare: bare, home: home, gitconfig: os.Getenv("GIT_CONFIG_GLOBAL")}
	remote.route(false)
	return remote
}

// AllowSSH makes the ssh URL reach the bare repository too.
func (r *GraphRemote) AllowSSH(t *testing.T) {
	t.Helper()
	r.route(true)
}

func (r *GraphRemote) route(sshWorks bool) {
	sshURL, httpsURL, err := model.GraphRemoteURLs(r.RepoID)
	if err != nil {
		panic(err)
	}
	sshTarget := filepath.Join(r.home, "unreachable.git")
	if sshWorks {
		sshTarget = r.Bare
	}
	config, err := os.ReadFile(r.gitconfig)
	if err != nil {
		panic(err)
	}
	var sections []string
	for line := range strings.Lines(string(config)) {
		if strings.HasPrefix(line, "[") || len(sections) == 0 {
			sections = append(sections, "")
		}
		sections[len(sections)-1] += line
	}
	var base strings.Builder
	for _, section := range sections {
		if strings.Contains(section, "insteadOf = "+httpsURL+"\n") || strings.Contains(section, "insteadOf = "+sshURL+"\n") {
			continue
		}
		base.WriteString(section)
	}
	rules := fmt.Sprintf("[url %q]\n\tinsteadOf = %s\n[url %q]\n\tinsteadOf = %s\n", "file://"+r.Bare, httpsURL, sshTarget, sshURL)
	err = os.WriteFile(r.gitconfig, []byte(base.String()+rules), 0o644)
	if err != nil {
		panic(err)
	}
}

// Clone makes a person's own clone of the remote and returns its path.
func (r *GraphRemote) Clone(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "own")
	Git(t, r.home, "clone", "-q", r.Bare, dir)
	return dir
}

// Commit writes files into a person's own clone of the remote, commits them
// with message and pushes, returning the pushed commit.
func (r *GraphRemote) Commit(t *testing.T, message string, files map[string]string) string {
	t.Helper()
	dir := r.Clone(t)
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		err := os.MkdirAll(filepath.Dir(path), 0o755)
		if err != nil {
			t.Fatal(err)
		}
		err = os.WriteFile(path, []byte(content), 0o644)
		if err != nil {
			t.Fatal(err)
		}
	}
	Git(t, dir, "add", "-A")
	Git(t, dir, "commit", "-q", "-m", message)
	Git(t, dir, "push", "-q", "origin", "HEAD:refs/heads/main")
	return strings.TrimSpace(Git(t, dir, "rev-parse", "HEAD"))
}

// Head is the commit the remote's main branch holds, empty when it has none.
func (r *GraphRemote) Head(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "-C", r.Bare, "rev-parse", "--verify", "--quiet", "refs/heads/main").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Show returns a file as the remote's main branch holds it, and false when it
// holds none.
func (r *GraphRemote) Show(t *testing.T, name string) (string, bool) {
	t.Helper()
	out, err := exec.Command("git", "-C", r.Bare, "show", "refs/heads/main:"+name).Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

// Rewrite replaces the remote's history by one commit holding files, as a
// force push does, and returns that commit.
func (r *GraphRemote) Rewrite(t *testing.T, message string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "rewrite")
	Git(t, r.home, "init", "-q", "-b", "main", dir)
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		err := os.MkdirAll(filepath.Dir(path), 0o755)
		if err != nil {
			t.Fatal(err)
		}
		err = os.WriteFile(path, []byte(content), 0o644)
		if err != nil {
			t.Fatal(err)
		}
	}
	Git(t, dir, "add", "-A")
	Git(t, dir, "commit", "-q", "-m", message)
	Git(t, dir, "push", "-q", "--force", r.Bare, "HEAD:refs/heads/main")
	return strings.TrimSpace(Git(t, dir, "rev-parse", "HEAD"))
}
