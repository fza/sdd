package handlers

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/networkteam/sdd/internal/command"
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

// setUpProject runs a plain init in projectRoot, which holds no sdd project
// yet, giving it the repository of its own it needs (20261029-130154-d-tac-frl).
// Every refusal comes before the first write.
func (h *Handler) setUpProject(ctx context.Context, cmd *command.InitCmd, projectRoot string, scope model.Scope, agents []model.AgentTarget) error {
	if h.projectRepos == nil || h.committerAt == nil {
		return fmt.Errorf("setting up the project at %s needs a git adapter for it", cmd.ProjectDir)
	}
	if within(projectRoot, cmd.RepoRoot) {
		return fmt.Errorf("--project-dir %s lies inside this repository; the project needs a repository of its own", cmd.ProjectDir)
	}
	root, err := h.projectRepos.RootOf(projectRoot)
	if err != nil {
		return err
	}
	if root != "" && !sameDir(root, projectRoot) {
		return fmt.Errorf("--project-dir %s lies inside the repository at %s; the project needs a repository of its own", cmd.ProjectDir, root)
	}
	graphDir := cmd.GraphDir
	if graphDir == "" {
		graphDir = model.DefaultGraphDir
	}
	absGraphDir := graphDir
	if !filepath.IsAbs(absGraphDir) {
		absGraphDir = filepath.Join(projectRoot, absGraphDir)
	}
	if !within(absGraphDir, projectRoot) {
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
	return h.runProjectInit(ctx, cmd, projectRoot, func(projectCmd *command.InitCmd) {
		projectCmd.Scope = scope
		projectCmd.ScopeExplicit = true
		projectCmd.Targets = agents
	})
}

// refreshProject runs the existing project's own init with the settings it
// records, so the project stays as current as the work repository
// (20261029-132740-d-tac-g32).
func (h *Handler) refreshProject(ctx context.Context, cmd *command.InitCmd, projectRoot string, projectCfg *model.PerRepoConfig) error {
	if h.projectRepos == nil || h.committerAt == nil {
		return fmt.Errorf("refreshing the project at %s needs a git adapter for it", cmd.ProjectDir)
	}
	return h.runProjectInit(ctx, cmd, projectRoot, func(projectCmd *command.InitCmd) {
		projectCmd.GraphDir = projectCfg.GraphDir
		projectCmd.ScopeExplicit = false
		projectCmd.Targets = nil
	})
}

// runProjectInit runs Init in projectRoot as its own repository, carrying the
// run's per-person answers and flags; adjust sets what differs between a
// set-up and a refresh.
func (h *Handler) runProjectInit(ctx context.Context, cmd *command.InitCmd, projectRoot string, adjust func(*command.InitCmd)) error {
	branch, err := h.projectRepos.CurrentBranch(projectRoot)
	if err != nil {
		return err
	}
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
	projectCmd.SetUpProject = false
	projectCmd.DefaultBranch = branch
	projectCmd.RemoteURL = h.projectRepos.RemoteURL(projectRoot)
	adjust(&projectCmd)
	err = project.Init(ctx, &projectCmd)
	if err != nil {
		return fmt.Errorf("initializing the project in %s: %w", projectRoot, err)
	}
	return nil
}

// within reports whether path is dir or lies below it, through symlinks such
// as macOS's /var → /private/var, resolving path's nearest existing ancestor
// when path does not exist yet.
func within(path, dir string) bool {
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		resolvedDir = filepath.Clean(dir)
	}
	rel, err := filepath.Rel(resolvedDir, resolveExisting(path))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// resolveExisting resolves symlinks in path's nearest existing ancestor and
// appends the rest unchanged.
func resolveExisting(path string) string {
	path = filepath.Clean(path)
	rest := ""
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return filepath.Join(path, rest)
		}
		rest = filepath.Join(filepath.Base(path), rest)
		path = parent
	}
}

// sameDir reports whether a and b name one directory, through symlinks.
func sameDir(a, b string) bool {
	return resolveExisting(a) == resolveExisting(b)
}
