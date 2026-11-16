package handlers

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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
	RootOf(dir string) (string, error)
	Init(dir string) error
	CurrentBranch(dir string) (string, error)
	RemoteURL(dir string) string
	StableRoot(dir string) (string, error)
}

// repoRootOf is ProjectRepositories.RootOf, empty when the handler reaches no
// repositories by path.
func (h *Handler) repoRootOf(dir string) (string, error) {
	if h.projectRepos == nil {
		return "", nil
	}
	return h.projectRepos.RootOf(dir)
}

// setUpProject runs a plain init in projectRoot, which holds no sdd project
// yet, giving it the repository of its own it needs. initPointer has run
// meta.RequireProjectApart; every refusal here comes before the first write.
func (h *Handler) setUpProject(ctx context.Context, cmd *command.InitCmd, projectRoot string, scope model.Scope, agents []model.AgentTarget, changed *bool) error {
	if h.projectRepos == nil || h.repositoryAt == nil {
		return fmt.Errorf("setting up the project at %s needs a git adapter for it", cmd.ProjectDir)
	}
	root, err := h.projectRepos.RootOf(projectRoot)
	if err != nil {
		return err
	}
	graphDir := cmd.GraphDir
	if graphDir == "" {
		graphDir = model.DefaultGraphDirFor(true)
	}
	absGraphDir := graphDir
	if !filepath.IsAbs(absGraphDir) {
		absGraphDir = filepath.Join(projectRoot, absGraphDir)
	}
	err = gitignoreLine(graphDir)
	if err != nil {
		return fmt.Errorf("--graph-dir: %w", err)
	}
	if !meta.WithinDir(projectRoot, absGraphDir) {
		return fmt.Errorf("--graph-dir %s leads out of the project at %s; the graph lives inside its project", cmd.GraphDir, cmd.ProjectDir)
	}
	_, err = os.Stat(filepath.Join(projectRoot, model.SDDDirName))
	if err == nil {
		return fmt.Errorf("%s holds a %s/ without config.yaml; restore its config.yaml or remove it", cmd.ProjectDir, model.SDDDirName)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	if root == "" {
		err = os.MkdirAll(projectRoot, 0o755)
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
	return h.runProjectInit(ctx, cmd, projectRoot, func(projectCmd *command.InitCmd) {
		projectCmd.DefaultBranch = branch
		projectCmd.GraphDir = graphDir
		projectCmd.Scope = scope
		projectCmd.ScopeExplicit = true
		projectCmd.Targets = agents
	}, changed)
}

// refreshProject runs the existing project's own init with the settings it
// records, so the project stays as current as the work repository.
func (h *Handler) refreshProject(ctx context.Context, cmd *command.InitCmd, projectRoot string, projectCfg *model.PerRepoConfig, changed *bool) error {
	if h.projectRepos == nil || h.repositoryAt == nil {
		return fmt.Errorf("refreshing the project at %s needs a git adapter for it", cmd.ProjectDir)
	}
	// default_branch is asked of git only when the project records none, so a
	// detached HEAD in the project blocks no refresh.
	var branch string
	if projectCfg.DefaultBranch == "" {
		var err error
		branch, err = h.projectRepos.CurrentBranch(projectRoot)
		if err != nil {
			return err
		}
	}
	return h.runProjectInit(ctx, cmd, projectRoot, func(projectCmd *command.InitCmd) {
		projectCmd.DefaultBranch = branch
		projectCmd.GraphDir = projectCfg.GraphDir
		projectCmd.Scope = ""
		projectCmd.ScopeExplicit = false
		projectCmd.Targets = nil
	}, changed)
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
	projectCmd.ProjectDir = ""
	projectCmd.GraphRepo = ""
	projectCmd.SetUpProject = false
	projectCmd.RemoteURL = h.projectRepos.RemoteURL(projectRoot)
	adjust(&projectCmd)
	err = project.initRepo(ctx, &projectCmd, changed)
	if err != nil {
		return fmt.Errorf("initializing the project in %s: %w", projectRoot, err)
	}
	return nil
}
