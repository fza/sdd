package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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

// setUpProject runs a plain init in projectRoot, which holds no sdd project
// yet, giving it the repository of its own it needs (20261029-130154-d-tac-frl).
func (h *Handler) setUpProject(ctx context.Context, cmd *command.InitCmd, projectRoot string, scope model.Scope, agents []model.AgentTarget) error {
	if h.projectRepos == nil || h.committerAt == nil {
		return fmt.Errorf("%s: %w; run `sdd init` there first", cmd.ProjectDir, meta.ErrNotAProject)
	}
	root := h.projectRepos.RootOf(projectRoot)
	if root == "" {
		err := os.MkdirAll(projectRoot, 0o755)
		if err != nil {
			return fmt.Errorf("creating %s: %w", projectRoot, err)
		}
		err = h.projectRepos.Init(projectRoot)
		if err != nil {
			return err
		}
	} else if !sameDir(root, projectRoot) || sameDir(root, cmd.RepoRoot) {
		return fmt.Errorf("--project-dir %s lies inside the repository at %s; the project needs a repository of its own", cmd.ProjectDir, root)
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
	err = project.Init(ctx, &projectCmd)
	if err != nil {
		return fmt.Errorf("setting up the project in %s: %w", projectRoot, err)
	}
	return nil
}

// sameDir reports whether a and b name one directory, through symlinks such as
// macOS's /var → /private/var.
func sameDir(a, b string) bool {
	resolvedA, err := filepath.EvalSymlinks(a)
	if err != nil {
		return false
	}
	resolvedB, err := filepath.EvalSymlinks(b)
	if err != nil {
		return false
	}
	return resolvedA == resolvedB
}
