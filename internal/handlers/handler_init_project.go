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
	// dir's nearest existing ancestor; empty when there is none. A repository
	// git cannot read is an error, never an empty root, so no setup runs
	// `git init` inside it.
	RootOf(dir string) (string, error)
	Init(dir string) error
	CurrentBranch(dir string) (string, error)
	RemoteURL(dir string) string
	StableRoot(dir string) (string, error)
}

// repoRoots returns RootOf for the path checks that take a plain lookup, and
// the first error such a lookup met, which the caller returns once they ran.
func (h *Handler) repoRoots() (rootOf func(dir string) string, failed func() error) {
	var first error
	rootOf = func(dir string) string {
		if h.projectRepos == nil {
			return ""
		}
		root, err := h.projectRepos.RootOf(dir)
		if err != nil && first == nil {
			first = err
		}
		return root
	}
	return rootOf, func() error { return first }
}

// setUpProject runs a plain init in projectRoot, which holds no sdd project
// yet, giving it the repository of its own it needs. Every refusal comes
// before the first write.
func (h *Handler) setUpProject(ctx context.Context, cmd *command.InitCmd, projectRoot string, scope model.Scope, agents []model.AgentTarget, changed *bool) error {
	if !cmd.SetUpProject || h.projectRepos == nil || h.committerAt == nil {
		return fmt.Errorf("%s leads to %s: %w; clone the project there, or set up a new one with `sdd init --project-dir %s`", cmd.ProjectDir, projectRoot, meta.ErrNotAProject, cmd.ProjectDir)
	}
	graphDirSetting := cmd.GraphDir
	if graphDirSetting == "" {
		graphDirSetting = model.DefaultGraphDirFor(true)
	}
	err := gitignoreLine(graphDirSetting)
	if err != nil {
		return fmt.Errorf("--graph-dir: %w", err)
	}
	graphDir := meta.ResolveGraphDir(projectRoot, &model.PerRepoConfig{GraphDir: graphDirSetting})
	if !meta.WithinDir(projectRoot, graphDir) {
		graphRepo, err := h.projectRepos.RootOf(graphDir)
		if err != nil {
			return err
		}
		if graphRepo == "" {
			return fmt.Errorf("--graph-dir leads to %s, outside the project and outside any repository; keep the graph inside the project", graphDir)
		}
		return fmt.Errorf("--graph-dir leads into %s, a repository separate from the project at %s; keep the graph inside the project", graphRepo, projectRoot)
	}
	_, err = os.Stat(filepath.Join(projectRoot, model.SDDDirName))
	if err == nil {
		return fmt.Errorf("%s holds a %s/ without config.yaml; restore its config.yaml or remove it", cmd.ProjectDir, model.SDDDirName)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	root, err := h.projectRepos.RootOf(projectRoot)
	if err != nil {
		return err
	}
	if root == "" {
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
	return h.runProjectInit(ctx, cmd, projectRoot, func(projectCmd *command.InitCmd) {
		projectCmd.DefaultBranch = branch
		projectCmd.GraphDir = graphDirSetting
		projectCmd.Scope = scope
		projectCmd.ScopeExplicit = true
		projectCmd.Targets = agents
	}, changed)
}

// refreshProject runs the existing project's own init with the settings it
// records, so the project stays as current as the work repository.
func (h *Handler) refreshProject(ctx context.Context, cmd *command.InitCmd, projectRoot string, projectCfg *model.PerRepoConfig, changed *bool) error {
	if h.projectRepos == nil || h.committerAt == nil {
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

// runProjectInit runs the init of projectRoot as its own repository, carrying
// the run's per-person answers and flags; adjust sets what differs between a
// set-up and a refresh.
func (h *Handler) runProjectInit(ctx context.Context, cmd *command.InitCmd, projectRoot string, adjust func(*command.InitCmd), changed *bool) error {
	stableRoot, err := h.projectRepos.StableRoot(projectRoot)
	if err != nil {
		return err
	}
	project := *h
	project.committer = h.committerAt(projectRoot)
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
