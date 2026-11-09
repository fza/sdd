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

func TestRewriteLinesCommentBlocks(t *testing.T) {
	remove := []string{".sdd/tmp/", ".sdd/stats/"}
	cases := []struct {
		name, content, want string
	}{
		{"an emptied block's heading goes with its blank line", "node_modules/\n\n# sdd\n.sdd/tmp/\n.sdd/stats/\n\n# backend\n/Data/\n", "node_modules/\n\n# backend\n/Data/\n"},
		{"an emptied first block takes the leading blank line", "# sdd\n.sdd/tmp/\n\n# backend\n/Data/\n", "# backend\n/Data/\n"},
		{"an emptied last block leaves no trailing heading", "node_modules/\n\n# sdd\n.sdd/tmp/\n", "node_modules/\n"},
		{"a block with no heading leaves no doubled blank line", "a\n\n.sdd/tmp/\n\nb\n", "a\n\nb\n"},
		{"an untouched comment-only block stays", "# notes\n\n# sdd\n.sdd/tmp/\n\nb\n", "# notes\n\nb\n"},
		{"a block keeping a pattern keeps its comments", "# sdd\n.sdd/tmp/\n.sdd/graph/x/\n\nb\n", "# sdd\n.sdd/graph/x/\n\nb\n"},
		{"a rewrite removing nothing keeps the content", "\n# notes\n\n\na\n\n", "\n# notes\n\n\na\n\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := string(model.RewriteLines([]byte(c.content), remove, nil)); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
	for _, content := range []string{"# sdd\n.sdd/tmp/\n", "\n# sdd\n.sdd/tmp/\n\n"} {
		if got := model.RewriteLines([]byte(content), remove, nil); got != nil {
			t.Errorf("a file left with only an emptied block and blank lines should be empty, got %q", got)
		}
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
