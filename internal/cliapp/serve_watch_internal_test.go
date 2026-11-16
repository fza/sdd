package cliapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/model"
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
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeWatched(t, path, "de")
	watch, err := newConfigWatch([]string{path}, func(_ *servedProject, reads []configRead) (*servedProject, error) {
		data := reads[0].data
		return &servedProject{
			sessions: &namedSessions{name: string(data)},
			layers:   model.ConfigLayers{Project: &model.PerRepoConfig{Language: string(data)}},
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return watch, path
}

func writeWatched(t *testing.T, path, content string) {
	t.Helper()
	err := os.WriteFile(path, []byte(content), 0o644)
	if err != nil {
		t.Fatal(err)
	}
}

// A call keeps the project it was admitted under while a later call swaps
// in another.
func TestConfigWatchPinsACallToOneProject(t *testing.T) {
	watch, path := languageWatch(t)
	first, _, err := watch.admit(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}

	writeWatched(t, path, "en")
	second, _, err := watch.admit(t.Context(), "")
	if err != nil {
		t.Fatal(err)
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

// Past the limit serve forgets the sessions told every notice, never one
// still owed a notice.
func TestConfigWatchForgetsToldSessionsPastTheLimit(t *testing.T) {
	watch, path := languageWatch(t)
	for _, language := range []string{"en", "fr"} {
		_, _, err := watch.admit(t.Context(), "owed")
		if err != nil {
			t.Fatal(err)
		}
		writeWatched(t, path, language)
		_, _, err = watch.admit(t.Context(), "")
		if err != nil {
			t.Fatal(err)
		}
	}

	for i := range toldLimit + 10 {
		_, _, err := watch.admit(t.Context(), fmt.Sprintf("s_bogus_%d", i))
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(watch.told) > toldLimit {
		t.Errorf("serve should remember at most the limit of sessions, remembers %d", len(watch.told))
	}
	_, notice, err := watch.admit(t.Context(), "owed")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notice, "language: en -> fr") || strings.Contains(notice, "language: de -> en") {
		t.Errorf("a session still owed a notice should be remembered and told only what it missed, got %q", notice)
	}
}

// A rebuild keeps the LLM runner and the embedders unless their keys
// changed.
func TestServedClientsFollowOnlyTheirOwnKeys(t *testing.T) {
	t.Chdir(t.TempDir())
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
		t.Setenv(name, filepath.Join(root, name))
	}
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
		layers                     model.ConfigLayers
		keepsRunner, keepsEmbedder bool
	}{
		{name: "unrelated key", layers: changed(func(l *model.ConfigLayers) { l.Project.Participant = "Ada" }), keepsRunner: true, keepsEmbedder: true},
		{name: "llm key", layers: changed(func(l *model.ConfigLayers) { l.Project.LLM.Model = "other-model" }), keepsEmbedder: true},
		{name: "embedding key", layers: changed(func(l *model.ConfigLayers) { l.Project.Embedding.Model = "other-embed" }), keepsRunner: true},
		{name: "global embedding hidden by the project", layers: changed(func(l *model.ConfigLayers) { l.Global.Embedding.Model = "other-global" }), keepsRunner: true},
	}

	command := &cli.Command{Name: "serve", Action: func(_ context.Context, cmd *cli.Command) error {
		clients, err := buildServedClients(cmd, nil, base)
		if err != nil {
			return err
		}
		previous := &servedProject{layers: base, clients: clients}
		for _, tc := range cases {
			next, err := buildServedClients(cmd, previous, tc.layers)
			if err != nil {
				return err
			}
			if (next.llm == clients.llm) != tc.keepsRunner {
				t.Errorf("%s: the runner should be kept only while no llm key changed", tc.name)
			}
			if (next.embedders == clients.embedders) != tc.keepsEmbedder {
				t.Errorf("%s: the embedders should be kept only while no embedding setting changed", tc.name)
			}
		}
		return nil
	}}
	err = command.Run(t.Context(), []string{"serve"})
	if err != nil {
		t.Fatal(err)
	}
}

// A rebuild derives the whole project from the one read its stamps came
// from: an edit landing after that read reaches neither the config, the
// layers nor the LLM runner.
func TestServedProjectComesFromOneRead(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
		t.Setenv(name, filepath.Join(root, name))
	}
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
		writeFile(t, filepath.Join(root, ".sdd", "config.yaml"), "default_branch: main\nlanguage: "+language+"\n")
		writeFile(t, filepath.Join(root, ".sdd", "config.local.yaml"), "llm:\n  provider: openai\n  model: "+llmModel+"\n  endpoint: "+server.URL+
			"\n  api_keys:\n    openai: op-notarealkeyaaaaaaaaaa\n  retry:\n    max_attempts: 1\n")
	}
	project("de", "read-model")
	paths, err := servedConfigFiles()
	if err != nil {
		t.Fatal(err)
	}
	reads := readConfigFiles(paths)
	project("en", "edited-model")

	command := &cli.Command{Name: "serve", Action: func(ctx context.Context, cmd *cli.Command) error {
		built, err := buildServedProject(ctx, cmd, nil, reads)
		if err != nil {
			return err
		}
		if built.cfg.Language != "de" || built.layers.Project.Language != "de" {
			t.Errorf("the config and the layers should come from the read, got %q and %q", built.cfg.Language, built.layers.Project.Language)
		}
		_, _ = built.clients.llm.Run(ctx, pkgllm.Request{UserPrompt: "ping"})
		mu.Lock()
		defer mu.Unlock()
		if len(models) == 0 || slices.ContainsFunc(models, func(model string) bool { return model != "read-model" }) {
			t.Errorf("the runner should call the model the read named, called %q", models)
		}
		return nil
	}}
	err = command.Run(t.Context(), []string{"serve"})
	if err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	writeWatched(t, path, content)
}

func TestServedConfigLayersNamesTheGlobalConfigOnAReadError(t *testing.T) {
	reads := make([]configRead, servedConfigCount)
	reads[globalConfigFile] = configRead{path: "/xdg/sdd/config.yaml", err: os.ErrPermission}
	_, err := servedConfigLayers(reads)
	if err == nil || !strings.Contains(err.Error(), "reading global config /xdg/sdd/config.yaml") {
		t.Errorf("a global read error should name the global config, got %v", err)
	}
}
