package model_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestRewriteLines(t *testing.T) {
	got := string(model.RewriteLines([]byte("keep\nold\n  old  \nshared\n"), []string{"old", "shared"}, []string{"shared", "new"}))
	if got != "keep\nshared\nnew\n" {
		t.Errorf("removed lines should go, kept and added lines stay once, got %q", got)
	}
	if model.RewriteLines([]byte("old\n"), []string{"old"}, nil) != nil {
		t.Error("a file left without lines should be empty")
	}
}

func TestMovedProjectConfig(t *testing.T) {
	source := []byte("# kept comment\ngraph_dir: .sdd/graph\ndefault_branch: main\nrepo_id: example.com/code\nproject_dir: ../x\nlanguage: de\nskill_scope: user\n")
	got, err := model.MovedProjectConfig(source, "graph", "trunk", "", &model.PerRepoConfig{SkillScope: model.ScopeProject, SupportedAgents: []model.AgentTarget{"claude", "codex"}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, want := range []string{"# kept comment", "graph_dir: graph", "default_branch: trunk", "language: de", "skill_scope: project", "supported_agents: [claude, codex]"} {
		if !strings.Contains(text, want) {
			t.Errorf("the moved config should hold %q:\n%s", want, text)
		}
	}
	for _, gone := range []string{"repo_id", "project_dir"} {
		if strings.Contains(text, gone) {
			t.Errorf("the moved config should drop %s:\n%s", gone, text)
		}
	}
}

func TestMergeLocalConfig(t *testing.T) {
	got, err := model.MergeLocalConfig([]byte("project_dir: ../graph\nllm:\n  model: kept\n"), []byte("participant: Ada\nllm:\n  model: moved\nproject_dir: ../other\n"), model.LocalPointerKeys)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.Contains(text, "participant: Ada") || !strings.Contains(text, "model: moved") || strings.Contains(text, "project_dir") || strings.Contains(text, "kept") {
		t.Errorf("the moved settings should win and the pointer keys leave both:\n%s", text)
	}
}

func TestLayoutLocation(t *testing.T) {
	project := &model.PerRepoConfig{GraphDir: "graph", DefaultBranch: "main"}
	cases := []struct {
		name    string
		pointer *model.PerRepoConfig
		project *model.PerRepoConfig
		want    string
	}{
		{name: "graph in its own repository", project: &model.PerRepoConfig{}, want: "git-dir:.sdd/graph"},
		{name: "project_dir", pointer: &model.PerRepoConfig{ProjectDir: "../g"}, project: project, want: "git-path:../g?dir=graph"},
		{name: "graph_repo", pointer: &model.PerRepoConfig{GraphRepo: "example.com/g", ProjectDir: "../g"}, project: project, want: "git-repo:example.com/g?branch=main&dir=graph"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.LayoutLocation(tc.pointer, tc.project).String(); got != tc.want {
				t.Errorf("LayoutLocation = %q, want %q", got, tc.want)
			}
		})
	}
}
