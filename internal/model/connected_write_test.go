package model_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

const (
	writerRepo = model.RepoID("github.com/acme/app")
	targetRepo = model.RepoID("github.com/acme/platform-sdd")
)

func parseLayer(t *testing.T, yaml string) *model.PerRepoConfig {
	t.Helper()
	cfg, err := model.ParseConfig([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestConnectedWriteRefusal(t *testing.T) {
	cases := []struct {
		name      string
		layers    []string
		inbound   string
		wantAllow bool
	}{
		{name: "both gates default to false", wantAllow: false},
		{name: "writing allowed, receiving silent", layers: []string{"connected:\n  write: true\n"}, wantAllow: false},
		{name: "receiving accepts, writing silent", inbound: "inbound:\n  accept: true\n", wantAllow: false},
		{name: "both catch-alls true", layers: []string{"connected:\n  write: true\n"}, inbound: "inbound:\n  accept: true\n", wantAllow: true},
		{
			name:      "the target's override beats a later catch-all false",
			layers:    []string{"connected:\n  repos:\n    github.com/acme/platform-sdd:\n      write: true\n", "connected:\n  write: false\n"},
			inbound:   "inbound:\n  accept: true\n",
			wantAllow: true,
		},
		{
			name:      "a later layer's override false beats an earlier override true",
			layers:    []string{"connected:\n  repos:\n    github.com/acme/platform-sdd:\n      write: true\n", "connected:\n  repos:\n    github.com/acme/platform-sdd:\n      write: false\n"},
			inbound:   "inbound:\n  accept: true\n",
			wantAllow: false,
		},
		{
			name:      "an override for another repository leaves the catch-all in charge",
			layers:    []string{"connected:\n  write: true\n  repos:\n    github.com/acme/other:\n      write: false\n"},
			inbound:   "inbound:\n  accept: true\n",
			wantAllow: true,
		},
		{
			name:      "the writer's inbound override beats the catch-all",
			layers:    []string{"connected:\n  write: true\n"},
			inbound:   "inbound:\n  accept: true\n  repos:\n    github.com/acme/app:\n      accept: false\n",
			wantAllow: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			merged := &model.PerRepoConfig{}
			for _, layer := range tc.layers {
				merged = model.MergeConfig(merged, parseLayer(t, layer))
			}
			receiving := parseLayer(t, tc.inbound)
			err := model.ConnectedWriteRefusal(writerRepo, merged.Connected, targetRepo, receiving.Inbound)
			if tc.wantAllow && err != nil {
				t.Errorf("the capture should be allowed: %v", err)
			}
			if !tc.wantAllow && !errors.Is(err, model.ErrConnectedWriteRefused) {
				t.Errorf("the capture should be refused, got %v", err)
			}
		})
	}
}

func TestRepoOverrideKey(t *testing.T) {
	path, err := model.RepoOverrideKey("connected.write", targetRepo)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(path, []string{"connected", "repos", string(targetRepo), "write"}) {
		t.Errorf("--repo should address the repository's override entry, got %v", path)
	}
	_, err = model.RepoOverrideKey("llm.model", targetRepo)
	if err == nil {
		t.Error("a key without per-repository overrides should be refused")
	}
	_, err = model.RepoOverrideKey("connected.write", "not-an-id")
	if err == nil {
		t.Error("a malformed repository id should be refused")
	}
}

func TestHonoredLocalDropsInbound(t *testing.T) {
	local := parseLayer(t, "inbound:\n  accept: true\nparticipant: x\n")
	honored := model.HonoredLocal(local)
	if honored.Inbound.Accepts(writerRepo) {
		t.Error("inbound set outside the committed config should not be honored")
	}
	if honored.Participant != "x" {
		t.Error("the rest of the local layer should stay")
	}
	if !local.Inbound.Accepts(writerRepo) {
		t.Error("the layer read should not be mutated")
	}
}

func TestHonoredLocalDropsRelevance(t *testing.T) {
	local := parseLayer(t, "relevance:\n  commit: true\n  content: reader\nparticipant: x\n")
	honored := model.HonoredLocal(local)
	if honored.Relevance != (model.RelevanceConfig{}) {
		t.Error("relevance set outside the committed config should not be honored")
	}
	if honored.Participant != "x" {
		t.Error("the rest of the local layer should stay")
	}
}
