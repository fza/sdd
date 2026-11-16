package cliapp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// namedSessions tells apart the session stores of two builds.
type namedSessions struct {
	sdd.SessionStore
	name string
}

// languageWatch watches one file and builds a project whose language and
// session store are named after the file's content.
func languageWatch(t *testing.T) (*configWatch, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	graphDir := filepath.Join(dir, "graph")
	writeWatched(t, path, "de")
	locate := func() (string, error) { return graphDir, nil }
	watch, err := newConfigWatch(locate, func(*servedProject) (*servedProject, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return &servedProject{
			sessions: &namedSessions{name: string(data)},
			layers:   model.ConfigLayers{Project: &model.PerRepoConfig{Language: string(data)}},
			graphDir: graphDir,
			watched:  []string{path},
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return watch, path
}

func writeWatched(t *testing.T, path, content string) {
	t.Helper()
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(path, []byte(content), 0o644)
	if err != nil {
		t.Fatal(err)
	}
}

// A call keeps the project it was admitted under while a later call swaps
// in another.
func TestConfigWatchPinsACallToOneProject(t *testing.T) {
	watch, path := languageWatch(t)
	first, _, err := watch.admit(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	writeWatched(t, path, "en")
	second, notice, err := watch.admit(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if notice == "" {
		t.Fatal("the edit should rebuild the project")
	}

	sessions := servedSessions{watch}
	if got := sessions.store(first).(*namedSessions).name; got != "de" {
		t.Errorf("a call should keep the project it was admitted under, got %q", got)
	}
	if got := sessions.store(second).(*namedSessions).name; got != "en" {
		t.Errorf("a call admitted after the change should get the new project, got %q", got)
	}
	if got := sessions.store(context.Background()).(*namedSessions).name; got != "en" {
		t.Errorf("outside a call the project served now should answer, got %q", got)
	}
}

// A rebuild keeps the LLM runner and the embedders unless their keys
// changed or the project moved.
func TestServedClientsFollowOnlyTheirOwnKeys(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
		t.Setenv(name, filepath.Join(root, name))
	}
	layout := meta.Project{Root: filepath.Join(root, "project"), WorkRoot: filepath.Join(root, "project")}
	base := model.ConfigLayers{
		Global:  model.BaseConfig{Embedding: model.EmbeddingConfig{Model: "global-embed"}},
		Project: &model.PerRepoConfig{Language: "de", BaseConfig: model.BaseConfig{Embedding: model.EmbeddingConfig{Model: "project-embed"}}},
	}
	changed := func(edit func(*model.ConfigLayers)) model.ConfigLayers {
		project := *base.Project
		layers := model.ConfigLayers{Global: base.Global, Project: &project}
		edit(&layers)
		return layers
	}
	cases := []struct {
		name                       string
		layout                     meta.Project
		layers                     model.ConfigLayers
		keepsRunner, keepsEmbedder bool
	}{
		{name: "unrelated key", layout: layout, layers: changed(func(l *model.ConfigLayers) { l.Project.Participant = "Ada" }), keepsRunner: true, keepsEmbedder: true},
		{name: "llm key", layout: layout, layers: changed(func(l *model.ConfigLayers) { l.Project.LLM.Model = "other-model" }), keepsEmbedder: true},
		{name: "embedding key", layout: layout, layers: changed(func(l *model.ConfigLayers) { l.Project.Embedding.Model = "other-embed" }), keepsRunner: true},
		{name: "global embedding hidden by the project", layout: layout, layers: changed(func(l *model.ConfigLayers) { l.Global.Embedding.Model = "other-global" }), keepsRunner: true},
		{name: "another project", layout: meta.Project{Root: filepath.Join(root, "other"), WorkRoot: layout.WorkRoot}, layers: base},
	}

	command := &cli.Command{Name: "serve", Action: func(_ context.Context, cmd *cli.Command) error {
		clients, err := buildServedClients(cmd, nil, layout, base, model.LLMConfig{})
		if err != nil {
			return err
		}
		previous := &servedProject{layout: layout, layers: base, clients: clients}
		for _, tc := range cases {
			next, err := buildServedClients(cmd, previous, tc.layout, tc.layers, model.LLMConfig{})
			if err != nil {
				return err
			}
			if (next.llm == clients.llm) != tc.keepsRunner {
				t.Errorf("%s: the runner should be kept only while no llm key changed in the same project", tc.name)
			}
			if (next.embedders == clients.embedders) != tc.keepsEmbedder {
				t.Errorf("%s: the embedders should be kept only while no embedding setting changed in the same project", tc.name)
			}
		}
		return nil
	}}
	err := command.Run(t.Context(), []string{"serve"})
	if err != nil {
		t.Fatal(err)
	}
}
