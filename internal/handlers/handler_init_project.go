package handlers

import (
	"context"
	"fmt"
	"os"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

// ProjectRepositories reaches the repository a work repository's init sets its
// project up in, beside the repository the init runs in.
type ProjectRepositories interface {
	// RootOf returns the root of the repository holding dir, resolved through
	// dir's nearest existing ancestor; empty when there is none.
	RootOf(dir string) string
	Init(dir string) error
	CurrentBranch(dir string) (string, error)
	RemoteURL(dir string) string
}

// repoRootOf returns the root of the repository holding dir; empty when there
// is none or the handler reaches no repositories by path.
func (h *Handler) repoRootOf(dir string) string {
	if h.projectRepos == nil {
		return ""
	}
	return h.projectRepos.RootOf(dir)
}

// setUpProject runs a plain init in projectRoot, which holds no sdd project
// yet, giving it the repository of its own it needs.
func (h *Handler) setUpProject(ctx context.Context, cmd *command.InitCmd, projectRoot string, scope model.Scope, agents []model.AgentTarget, changed *bool) error {
	if !cmd.SetUpProject || h.projectRepos == nil || h.committerAt == nil {
		return fmt.Errorf("%s leads to %s: %w; clone the project there, or set up a new one with `sdd init --project-dir %s`", cmd.ProjectDir, projectRoot, meta.ErrNotAProject, cmd.ProjectDir)
	}
	err := gitignoreLine(cmd.GraphDir)
	if err != nil {
		return fmt.Errorf("--graph-dir: %w", err)
	}
	graphDir := meta.ResolveGraphDir(projectRoot, &model.PerRepoConfig{GraphDir: cmd.GraphDir})
	if !meta.WithinDir(projectRoot, graphDir) {
		graphRepo := h.projectRepos.RootOf(graphDir)
		if graphRepo == "" {
			return fmt.Errorf("--graph-dir leads to %s, outside the project and outside any repository; keep the graph inside the project", graphDir)
		}
		return fmt.Errorf("--graph-dir leads into %s, a repository separate from the project at %s; keep the graph inside the project", graphRepo, projectRoot)
	}
	if h.projectRepos.RootOf(projectRoot) == "" {
		err := os.MkdirAll(projectRoot, 0o755)
		if err != nil {
			return fmt.Errorf("creating %s: %w", projectRoot, err)
		}
		err = h.projectRepos.Init(projectRoot)
		if err != nil {
			return err
		}
	}

	branch, err := h.projectRepos.CurrentBranch(projectRoot)
	if err != nil {
		return err
	}
	project := *h
	project.committer = h.committerAt(projectRoot)
	projectCmd := *cmd
	projectCmd.RepoRoot = projectRoot
	projectCmd.StableRepoRoot = ""
	projectCmd.ProjectDir = ""
	projectCmd.DefaultBranch = branch
	projectCmd.RemoteURL = h.projectRepos.RemoteURL(projectRoot)
	projectCmd.Scope = scope
	projectCmd.ScopeExplicit = true
	projectCmd.Targets = agents
	err = project.initRepo(ctx, &projectCmd, changed)
	if err != nil {
		return fmt.Errorf("setting up the project in %s: %w", projectRoot, err)
	}
	return nil
}
