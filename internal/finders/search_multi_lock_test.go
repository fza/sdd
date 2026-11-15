package finders_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/flock"

	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/index"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/internal/repos/repostest"
	"github.com/networkteam/sdd/pkg/llm/embed"
)

func writeMemberEntry(t *testing.T, dir, id, body string) {
	t.Helper()
	rel, err := model.IDToRelPath(id)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\ntype: signal\nlayer: tac\nkind: gap\n---\n\n"+body+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A member index another process holds past the lock bound is searched by
// text, as the local index is, instead of waiting out the writer.
func TestMultiSearchReadsAHeldMemberIndexByText(t *testing.T) {
	const repoID = "example.com/team/other"
	loc := repos.Locations{ConfigPath: filepath.Join(t.TempDir(), "config.yaml"), CacheRoot: t.TempDir()}
	reg := repos.NewRegistry(loc)
	localDir := t.TempDir()
	writeMemberEntry(t, localDir, "20260601-120000-s-tac-loc", "A local gap about pipelines.")
	cacheDir, err := reg.CacheDir(repoID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cacheDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMemberEntry(t, filepath.Join(cacheDir, ".sdd/graph"), "20260601-130000-s-tac-rem", "A remote gap about telemetry dashboards.")
	cfg := &repos.GlobalConfig{}
	if err := cfg.AddRepo(repos.ConnectedRepo{RepoID: repoID, CloneURL: "https://" + repoID}); err != nil {
		t.Fatal(err)
	}
	repostest.WriteConfig(t, loc.ConfigPath, cfg)

	embedder := embed.EmbedderFunc{Space: "test/space", Run: func(_ context.Context, req embed.Request) (embed.Result, error) {
		result := embed.Result{}
		for range req.Texts {
			result.Vectors = append(result.Vectors, []float32{1, 0})
		}
		return result, nil
	}}
	memberStore := index.StoreDir(reg.CacheRoot(), index.RepoKey(repoID, cacheDir), embedder.Fingerprint())
	if err := os.MkdirAll(memberStore, 0o755); err != nil {
		t.Fatal(err)
	}
	held := flock.New(filepath.Join(memberStore, ".lock"))
	if err := held.Lock(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Unlock() }()

	f := finders.New(finders.Options{Repos: reg, Config: &model.PerRepoConfig{RepoID: "example.com/team/local", Dependencies: []model.RepoID{repoID}}})
	g, err := f.CurrentGraph(localDir)
	if err != nil {
		t.Fatal(err)
	}
	closure, err := f.DependencyClosure(t.Context(), query.DependencyClosureQuery{})
	if err != nil {
		t.Fatal(err)
	}
	local := finders.NewSearchFinder(finders.SearchFinderOptions{
		Graph: g, GraphDir: localDir, Repos: reg, Closure: closure,
		Embedder: embedder, IndexStore: index.OpenInMemory(), IndexLockBound: 100 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	res, err := finders.MultiSearch(ctx, local, query.SearchQuery{Phrase: "telemetry dashboards", AllRepos: true})
	if err != nil {
		t.Fatalf("a search should not wait out a held member index: %v", err)
	}
	for _, entry := range res.Entries {
		if entry.DisplayID() == repoID+":20260601-130000-s-tac-rem" {
			return
		}
	}
	t.Errorf("the held member should be searched by text, found %+v", res.Entries)
}
