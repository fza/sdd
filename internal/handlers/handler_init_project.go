package handlers

import (
	"context"
	"fmt"

	"github.com/networkteam/sdd/internal/command"
)

// ProjectRepositories reaches the repository holding a code repository's
// graph, beside the repository the command runs in.
type ProjectRepositories interface {
	// RootOf returns the root of the repository holding dir, resolved through
	// dir's nearest existing ancestor; empty when there is none.
	RootOf(dir string) (string, error)
	CurrentBranch(dir string) (string, error)
	RemoteURL(dir string) string
	StableRoot(dir string) (string, error)
}

// runProjectInit runs Init in projectRoot as its own repository, carrying the
// run's per-person answers and flags; adjust sets what differs between a
// set-up and a refresh.
func (h *Handler) runProjectInit(ctx context.Context, cmd *command.InitCmd, projectRoot string, adjust func(*command.InitCmd), changed *bool) error {
	stableRoot, err := h.projectRepos.StableRoot(projectRoot)
	if err != nil {
		return err
	}
	project := *h
	repository := h.repositoryAt(projectRoot)
	project.committer = repository
	project.tracker = repository
	projectCmd := *cmd
	projectCmd.RepoRoot = projectRoot
	projectCmd.StableRepoRoot = stableRoot
	projectCmd.GraphRepo = ""
	projectCmd.RemoteURL = h.projectRepos.RemoteURL(projectRoot)
	adjust(&projectCmd)
	err = project.initRepo(ctx, &projectCmd, changed)
	if err != nil {
		return fmt.Errorf("initializing the project in %s: %w", projectRoot, err)
	}
	return nil
}
