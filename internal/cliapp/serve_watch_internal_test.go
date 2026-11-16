package cliapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/secrets"
	sdd "github.com/networkteam/sdd/pkg/application"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
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
	locate := func(*configFiles) (string, error) { return graphDir, nil }
	watch, err := newConfigWatch(locate, func(files *configFiles, _ *servedProject) (*servedProject, error) {
		data, err := files.read(path)
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
		clients, err := buildServedClients(cmd, nil, servedConfig{project: layout, layers: base}, model.LLMConfig{})
		if err != nil {
			return err
		}
		previous := &servedProject{layout: layout, layers: base, clients: clients}
		for _, tc := range cases {
			next, err := buildServedClients(cmd, previous, servedConfig{project: tc.layout, layers: tc.layers}, model.LLMConfig{})
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

// servedFixture is a project at a fresh working directory, with sdd's
// machine folders inside it, and the paths of its global, committed and
// local config.
func servedFixture(t *testing.T) (global, committed, local string) {
	t.Helper()
	root := canonicalTempDir(t)
	t.Chdir(root)
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
		t.Setenv(name, filepath.Join(root, name))
	}
	return filepath.Join(root, "XDG_CONFIG_HOME", "sdd", "config.yaml"), filepath.Join(root, ".sdd", "config.yaml"), filepath.Join(root, ".sdd", "config.local.yaml")
}

// A rebuild derives the whole project from the one read its stamps came
// from: an edit landing after that read reaches neither the config, the
// layers nor the LLM runner.
func TestServedConfigComesFromOneRead(t *testing.T) {
	global, committed, local := servedFixture(t)
	var (
		mu     sync.Mutex
		models []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		models = append(models, req.Model)
		mu.Unlock()
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	project := func(language, llmModel string) {
		writeWatched(t, committed, "default_branch: main\nlanguage: "+language+"\n")
		writeWatched(t, local, "llm:\n  provider: openai\n  model: "+llmModel+"\n  endpoint: "+server.URL+
			"\n  api_keys:\n    openai: op-notarealkeyaaaaaaaaaa\n  retry:\n    max_attempts: 1\n")
	}
	project("de", "read-model")
	files := newConfigFiles()
	files.stamps([]string{global, committed, local})
	project("en", "edited-model")

	command := &cli.Command{Name: "serve", Action: func(ctx context.Context, cmd *cli.Command) error {
		served, err := resolveServedConfig(cmd, files)
		if err != nil {
			return err
		}
		if served.cfg.Language != "de" || served.layers.Project.Language != "de" {
			t.Errorf("the config and the layers should come from the read, got %q and %q", served.cfg.Language, served.layers.Project.Language)
		}
		clients, err := buildServedClients(cmd, nil, served, runnerConfigFrom(cmd, "", served.cfg))
		if err != nil {
			return err
		}
		_, _ = clients.llm.Run(secrets.WithNoNewParts(ctx), pkgllm.Request{UserPrompt: "ping"})
		mu.Lock()
		defer mu.Unlock()
		if len(models) == 0 || slices.ContainsFunc(models, func(model string) bool { return model != "read-model" }) {
			t.Errorf("the runner should call the model the read named, called %q", models)
		}
		return nil
	}}
	err := command.Run(t.Context(), []string{"serve"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestServedConfigNamesTheGlobalConfigOnAReadError(t *testing.T) {
	global, committed, _ := servedFixture(t)
	writeWatched(t, committed, "default_branch: main\n")
	files := newConfigFiles()
	files.reads[filepath.Clean(global)] = configRead{err: os.ErrPermission}

	command := &cli.Command{Name: "serve", Action: func(_ context.Context, cmd *cli.Command) error {
		_, err := resolveServedConfig(cmd, files)
		if err == nil || !strings.Contains(err.Error(), "reading global config "+global) {
			t.Errorf("a global read error should name the global config, got %v", err)
		}
		return nil
	}}
	err := command.Run(t.Context(), []string{"serve"})
	if err != nil {
		t.Fatal(err)
	}
}
