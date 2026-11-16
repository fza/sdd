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

// LocalConfigPath is the file of the project's machine-local config layer,
// the one `sdd config set --local` writes: override (--local-config) when
// set, else, for a pointed project, WorkRoot's config.local.yaml, which lies
// over the project's own (UnderLocalConfigPath); empty resolves it under
// SDDDir.
func (p Project) LocalConfigPath(override string) string {
	if override == "" && p.Pointed() {
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
	committed, local, err := readLayoutLayers(sddDir, localPath)
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
	// A committed path is every clone's, so it may not name one machine's folder.
	if committed != nil && filepath.IsAbs(committed.ProjectDir) {
		return Project{}, fmt.Errorf("%s sets project_dir %s; an absolute project_dir belongs in %s, the committed one is relative to the repository", committedPath, committed.ProjectDir, localPath)
	}
	root := projectDir
	if !filepath.IsAbs(root) {
		root = filepath.Join(workRoot, root)
	}
	root = filepath.Clean(root)
	targetCommitted, targetLocal, err := readLayoutLayers(SDDDir(root), "")
	if err != nil {
		return Project{}, err
	}
	if targetCommitted == nil {
		return Project{}, fmt.Errorf("project_dir %s in %s: %w; clone the project there, or set up a new one with `sdd init --project-dir %s`", projectDir, sddDir, ErrNotAProject, projectDir)
	}
	if model.MergeConfig(targetCommitted, targetLocal).ProjectDir != "" {
		return Project{}, fmt.Errorf("project_dir %s in %s names a repository that itself sets project_dir; point at the project holding the graph", projectDir, sddDir)
	}
	return Project{Root: root, WorkRoot: workRoot}, nil
}

// ResolveSwitchedProject resolves workRoot as the graph_repo project repoID
// names when its config points at a graph already, through graph_repo or
// project_dir: `sdd init --graph-repo` switches such a repository, whose
// machine-local layer holds the settings of a graph_repo project before its
// committed config names one. Any other workRoot resolves as ResolveProject
// resolves it.
func ResolveSwitchedProject(workRoot string, repoID model.RepoID, graphClone func(model.RepoID) (string, error)) (Project, error) {
	sddDir := SDDDir(workRoot)
	committed, local, err := readLayoutLayers(sddDir, LocalConfigPath(sddDir, ""))
	if err != nil {
		return Project{}, err
	}
	pointers := model.MergeConfig(committed, local)
	if pointers.GraphRepo == "" && pointers.ProjectDir == "" {
		return ResolveProject(workRoot, graphClone)
	}
	return resolveGraphRepo(workRoot, repoID, filepath.Join(sddDir, "config.yaml"), graphClone)
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

// RootOf returns the root of the repository holding path, empty when there is
// none.
type RootOf func(path string) (string, error)

// RequireProjectApart refuses a project at projectRoot that a work tree at
// workRoot cannot point at: one inside the repository holding the work tree,
// a repository of its own nested there included; one containing the work
// tree; or one inside another repository rather than at its root. subject
// opens the refusal, naming where the project came from.
func RequireProjectApart(subject, workRoot, projectRoot string, rootOf RootOf) error {
	outer, err := rootOf(workRoot)
	if err != nil {
		return err
	}
	if outer == "" {
		outer = workRoot
	}
	if WithinDir(outer, projectRoot) {
		return fmt.Errorf("%s lies inside the repository at %s; the project needs a repository of its own", subject, outer)
	}
	if WithinDir(projectRoot, workRoot) {
		return fmt.Errorf("%s contains the work repository at %s; the project needs a repository of its own beside it", subject, workRoot)
	}
	projectRepo, err := rootOf(projectRoot)
	if err != nil {
		return err
	}
	if projectRepo != "" && !SameDir(projectRepo, projectRoot) {
		return fmt.Errorf("%s lies inside the repository at %s; the project needs a repository of its own", subject, projectRepo)
	}
	return nil
}

// RequireProjectRepository refuses an existing project at projectRoot that
// lies in no repository: its graph commits would have nowhere to land.
func RequireProjectRepository(subject, projectRoot string, rootOf RootOf) error {
	root, err := rootOf(projectRoot)
	if err != nil || root != "" {
		return err
	}
	return fmt.Errorf("%s lies outside any repository; the project needs a repository of its own: run `git init` there and commit its .sdd/", subject)
}

// RequireGraphInProjectRepository refuses a graph_dir, set in sddDir and
// resolved to graphDir, that leads out of the project at projectRoot into no
// repository or into one other than the project's: graph commits, the schema
// stamp and engine branches all belong to the repository holding the
// project's .sdd/.
func RequireGraphInProjectRepository(sddDir, projectRoot, graphDir string, rootOf RootOf) error {
	if WithinDir(projectRoot, graphDir) {
		return nil
	}
	graphRepo, err := rootOf(graphDir)
	if err != nil {
		return err
	}
	if graphRepo == "" {
		return fmt.Errorf("graph_dir in %s leads to %s, outside the project and outside any repository; keep the graph inside the project", sddDir, graphDir)
	}
	projectRepo, err := rootOf(projectRoot)
	if err != nil {
		return err
	}
	if projectRepo == "" || SameDir(graphRepo, projectRepo) {
		return nil
	}
	return fmt.Errorf("graph_dir in %s leads into %s, a repository separate from %s; make the graph a project of its own (`sdd init` there) and set project_dir to that repository's root instead", sddDir, graphRepo, projectRepo)
}

// WithinDir reports whether path is dir or lies below it. Folders on disk
// compare by identity, so a symlink or a spelling a case-insensitive
// filesystem accepts names the folder it leads to; the parts of either path
// not yet created compare by name.
func WithinDir(dir, path string) bool {
	dirBase, dirRest := splitExisting(ResolvedPath(dir))
	dirInfo, err := os.Stat(dirBase)
	if err != nil {
		return false
	}
	ancestor := ResolvedPath(path)
	var below []string
	for {
		info, err := os.Stat(ancestor)
		if err == nil && os.SameFile(dirInfo, info) {
			return len(below) >= len(dirRest) && slices.Equal(below[:len(dirRest)], dirRest)
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return false
		}
		below = append([]string{filepath.Base(ancestor)}, below...)
		ancestor = parent
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

// splitExisting splits path into its nearest existing ancestor and the names
// below it.
func splitExisting(path string) (string, []string) {
	var rest []string
	for {
		_, err := os.Stat(path)
		if err == nil {
			return path, rest
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path, rest
		}
		rest = append([]string{filepath.Base(path)}, rest...)
		path = parent
	}
}
