package repos_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/repos"
)

// remoteURLs answers each clone directory's remote URL and counts the asks.
type remoteURLs struct {
	urls map[string]string
	asks int
}

func (r *remoteURLs) RemoteURL(_ context.Context, dir string) (string, error) {
	r.asks++
	return r.urls[dir], nil
}

// The git gate's clones are the sdd-owned clones under the data root, by the
// remote each one fetches from: a directory in staging, one without a clone
// and a clone of a local path do not count.
func TestCloneTargets(t *testing.T) {
	loc := repos.Locations{DataRoot: t.TempDir()}
	root := filepath.Join(loc.DataRoot, "repos")
	urls := &remoteURLs{urls: map[string]string{}}
	clone := func(rel, url string) {
		t.Helper()
		dir := filepath.Join(root, filepath.FromSlash(rel))
		err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
		if err != nil {
			t.Fatal(err)
		}
		err = os.WriteFile(filepath.Join(dir, ".git", "config"), nil, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		urls.urls[dir] = url
	}
	clone("gitlab.com/group/app-sdd", "git@gitlab.com:group/app-sdd.git")
	clone("github.com/acme/deep/platform", "https://github.com/acme/deep/platform.git")
	clone("gitlab.com/group/local", "/srv/graphs/local.git")
	clone("gitlab.com/group/next.cloning", "git@gitlab.com:group/next.git")
	err := os.MkdirAll(filepath.Join(root, "gitlab.com", "group", "empty"), 0o755)
	if err != nil {
		t.Fatal(err)
	}

	targets := repos.NewCloneTargets(loc, urls)
	got, err := targets.Targets(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, target := range got {
		names = append(names, target.Repo)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"github.com/acme/deep/platform", "gitlab.com/group/app-sdd"}) {
		t.Errorf("only the sdd-owned clones of remote hosts should count, got %q", names)
	}
	if slices.ContainsFunc(got, func(target gitgate.Target) bool { return target.Protocol == gitgate.Local }) {
		t.Error("a clone should carry its remote's protocol")
	}

	asked := urls.asks
	_, err = targets.Targets(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if urls.asks != asked {
		t.Error("a clone's remote should be read once while its git config is unchanged")
	}
}

func TestCloneTargetsWithoutClones(t *testing.T) {
	got, err := repos.NewCloneTargets(repos.Locations{DataRoot: t.TempDir()}, &remoteURLs{}).Targets(t.Context())
	if err != nil || len(got) != 0 {
		t.Errorf("a machine without clones should list none, got %v, %v", got, err)
	}
}
