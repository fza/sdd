package meta

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/networkteam/sdd/internal/model"
)

var ErrNotAProject = errors.New("not an sdd project")

// Project locates the two roots a command can need. WorkRoot holds the .sdd/
// found from the working directory: skills, agent registrations and the work
// itself live there. Root holds the .sdd/ every project setting, the graph and
// runtime state come from. They differ when WorkRoot's config sets
// project_dir.
type Project struct {
	Root     string
	WorkRoot string
	// GraphRepo is the graph repository WorkRoot's config names; Root is then
	// its sdd-owned clone.
	GraphRepo model.RepoID
}

func (p Project) SDDDir() string { return SDDDir(p.Root) }

func (p Project) Pointed() bool { return p.Root != p.WorkRoot }

// LocalConfigPath is the file of the project's machine-local config layer:
// override (--local-config) when set, else, for a project reached through
// graph_repo, WorkRoot's config.local.yaml, since the upkeep of sdd's clone
// removes any local file there (20261106-202055-d-tac-eyl); empty resolves it
// under SDDDir.
func (p Project) LocalConfigPath(override string) string {
	if override == "" && p.GraphRepo != "" {
		return LocalConfigPath(SDDDir(p.WorkRoot), "")
	}
	return override
}

// ResolveProject follows workRoot's graph_repo, when its config sets one, to
// the sdd-owned clone graphClone names, else its project_dir to the project
// holding the graph. A pointing repository carries only the pointer keys, and
// the project_dir it names must be one that does not point on.
func ResolveProject(workRoot string, graphClone func(model.RepoID) (string, error)) (Project, error) {
	sddDir := SDDDir(workRoot)
	committedPath := filepath.Join(sddDir, "config.yaml")
	localPath := LocalConfigPath(sddDir, "")
	committed, local, err := ReadConfigLayers(sddDir, localPath)
	if err != nil {
		return Project{}, err
	}
	pointers := model.MergeConfig(committed, local)
	if pointers.GraphRepo != "" {
		return resolveGraphRepo(workRoot, pointers.GraphRepo, committedPath, graphClone)
	}
	projectDir := pointers.ProjectDir
	if projectDir == "" {
		return Project{Root: workRoot, WorkRoot: workRoot}, nil
	}
	err = requireOnlyKeys(committedPath, model.PointerKeys)
	if err != nil {
		return Project{}, err
	}
	err = requireOnlyKeys(localPath, model.LocalPointerKeys)
	if err != nil {
		return Project{}, err
	}
	root := projectDir
	if !filepath.IsAbs(root) {
		root = filepath.Join(workRoot, root)
	}
	root = filepath.Clean(root)
	target, err := readConfigFile(filepath.Join(SDDDir(root), "config.yaml"))
	if err != nil {
		return Project{}, err
	}
	if target == nil {
		return Project{}, fmt.Errorf("project_dir %s in %s: %w; clone the project there, or set up a new one with `sdd init --project-dir %s`", projectDir, sddDir, ErrNotAProject, projectDir)
	}
	if target.ProjectDir != "" {
		return Project{}, fmt.Errorf("project_dir %s in %s names a repository that itself sets project_dir; point at the project holding the graph", projectDir, sddDir)
	}
	return Project{Root: root, WorkRoot: workRoot}, nil
}

func resolveGraphRepo(workRoot string, repoID model.RepoID, committedPath string, graphClone func(model.RepoID) (string, error)) (Project, error) {
	err := model.ValidateRepoID(repoID)
	if err != nil {
		return Project{}, fmt.Errorf("graph_repo in %s: %w", SDDDir(workRoot), err)
	}
	err = requireOnlyKeys(committedPath, model.GraphRepoPointerKeys)
	if err != nil {
		return Project{}, err
	}
	if graphClone == nil {
		return Project{}, fmt.Errorf("graph_repo in %s: no place for graph clones configured", SDDDir(workRoot))
	}
	root, err := graphClone(repoID)
	if err != nil {
		return Project{}, fmt.Errorf("graph_repo in %s: %w", SDDDir(workRoot), err)
	}
	return Project{Root: root, WorkRoot: workRoot, GraphRepo: repoID}, nil
}

func requireOnlyKeys(path string, allowed []string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	keys, err := model.TopLevelKeys(data)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	var misplaced []string
	for _, key := range keys {
		if !slices.Contains(allowed, key) {
			misplaced = append(misplaced, key)
		}
	}
	if len(misplaced) == 0 {
		return nil
	}
	return fmt.Errorf("%s sets %s, but a repository with %s may set only %s; move the rest into the project's own .sdd/ config", path, strings.Join(misplaced, ", "), allowed[0], strings.Join(allowed, ", "))
}
