package meta

import (
	"errors"
	"fmt"
	"io/fs"
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
// graph_repo.
type Project struct {
	Root     string
	WorkRoot string
	// GraphRepo is the graph repository WorkRoot's config names; Root is then
	// its sdd-owned clone.
	GraphRepo model.RepoID
}

func (p Project) SDDDir() string { return SDDDir(p.Root) }

func (p Project) Pointed() bool { return p.Root != p.WorkRoot }

// LocalConfigPath is the file of the project's machine-local config layer,
// the one `sdd config set --local` writes: override (--local-config) when
// set, else, for a pointed project, WorkRoot's config.local.yaml; empty
// resolves it under SDDDir.
func (p Project) LocalConfigPath(override string) string {
	if override == "" && p.Pointed() {
		return LocalConfigPath(SDDDir(p.WorkRoot), "")
	}
	return override
}

// ResolveProject follows workRoot's graph_repo, when its config sets one, to
// the sdd-owned clone graphClone names. A pointing repository carries only
// the pointer keys.
func ResolveProject(workRoot string, graphClone func(model.RepoID) (string, error)) (Project, error) {
	return ResolveProjectWith(os.ReadFile, workRoot, graphClone)
}

// ResolveProjectWith is ResolveProject reading the config files through read.
func ResolveProjectWith(read ReadFile, workRoot string, graphClone func(model.RepoID) (string, error)) (Project, error) {
	sddDir := SDDDir(workRoot)
	committed, local, err := readLayoutLayers(read, sddDir, LocalConfigPath(sddDir, ""))
	if err != nil {
		return Project{}, err
	}
	pointers := model.MergeConfig(committed, local)
	if pointers.GraphRepo != "" {
		return resolveGraphRepo(read, workRoot, pointers.GraphRepo, filepath.Join(sddDir, "config.yaml"), graphClone)
	}
	return Project{Root: workRoot, WorkRoot: workRoot}, nil
}

// ResolveSwitchedProject resolves workRoot as the graph_repo project repoID
// names when its config names a graph repository already: `sdd init
// --graph-repo` switches such a repository to repoID. Any other workRoot
// resolves as ResolveProject resolves it.
func ResolveSwitchedProject(workRoot string, repoID model.RepoID, graphClone func(model.RepoID) (string, error)) (Project, error) {
	return ResolveSwitchedProjectWith(os.ReadFile, workRoot, repoID, graphClone)
}

// ResolveSwitchedProjectWith is ResolveSwitchedProject reading the config
// files through read.
func ResolveSwitchedProjectWith(read ReadFile, workRoot string, repoID model.RepoID, graphClone func(model.RepoID) (string, error)) (Project, error) {
	sddDir := SDDDir(workRoot)
	committed, local, err := readLayoutLayers(read, sddDir, LocalConfigPath(sddDir, ""))
	if err != nil {
		return Project{}, err
	}
	pointers := model.MergeConfig(committed, local)
	if pointers.GraphRepo == "" {
		return ResolveProjectWith(read, workRoot, graphClone)
	}
	return resolveGraphRepo(read, workRoot, repoID, filepath.Join(sddDir, "config.yaml"), graphClone)
}

func resolveGraphRepo(read ReadFile, workRoot string, repoID model.RepoID, committedPath string, graphClone func(model.RepoID) (string, error)) (Project, error) {
	err := model.ValidateRepoID(repoID)
	if err != nil {
		return Project{}, fmt.Errorf("graph_repo in %s: %w", SDDDir(workRoot), err)
	}
	err = requireOnlyKeys(read, committedPath, model.GraphRepoPointerKeys)
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

// RefuseNestedGraphRepo refuses a graph repository whose own committed config
// names a graph repository: project.Root then holds no graph to work on.
func RefuseNestedGraphRepo(read ReadFile, project Project) error {
	if project.GraphRepo == "" {
		return nil
	}
	path := filepath.Join(SDDDir(project.Root), "config.yaml")
	cfg, err := readConfigFile(read, path)
	if err != nil || cfg == nil || cfg.GraphRepo == "" {
		return err
	}
	return fmt.Errorf("graph repository %s sets graph_repo %s in %s; a graph repository holds its graph itself: remove the key there, or name %s as graph_repo", project.GraphRepo, cfg.GraphRepo, path, cfg.GraphRepo)
}

func requireOnlyKeys(read ReadFile, path string, allowed []string) error {
	data, err := read(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
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
		if !slices.Contains(allowed, key) && !slices.Contains(model.IgnoredPointerKeys, key) {
			misplaced = append(misplaced, key)
		}
	}
	if len(misplaced) == 0 {
		return nil
	}
	return fmt.Errorf("%s sets %s, but a repository with %s may set only %s; move the rest into the project's own .sdd/ config", path, strings.Join(misplaced, ", "), allowed[0], strings.Join(allowed, ", "))
}

// ResolvedPath resolves symlinks in path, through its nearest existing
// ancestor when path does not exist yet.
func ResolvedPath(path string) string {
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

// SameDir reports whether a and b name one folder on disk.
func SameDir(a, b string) bool {
	infoA, err := os.Stat(a)
	if err != nil {
		return false
	}
	infoB, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(infoA, infoB)
}
