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
}

func (p Project) SDDDir() string { return SDDDir(p.Root) }

func (p Project) Pointed() bool { return p.Root != p.WorkRoot }

// ResolveProject follows workRoot's project_dir, when its config sets one, to
// the project holding the graph. A pointing repository carries only the
// pointer keys, and the project it names must be one that does not point on.
func ResolveProject(workRoot string) (Project, error) {
	sddDir := SDDDir(workRoot)
	committedPath := filepath.Join(sddDir, "config.yaml")
	localPath := filepath.Join(sddDir, "config.local.yaml")
	committed, local, err := ReadConfigLayers(sddDir)
	if err != nil {
		return Project{}, err
	}
	projectDir := model.MergeConfig(committed, local).ProjectDir
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
	// A committed path is every clone's, so it may not name one machine's folder.
	if committed != nil && filepath.IsAbs(committed.ProjectDir) {
		return Project{}, fmt.Errorf("%s sets project_dir %s; an absolute project_dir belongs in %s, the committed one is relative to the repository", committedPath, committed.ProjectDir, localPath)
	}
	root := projectDir
	if !filepath.IsAbs(root) {
		// init records project_dir against the real folders.
		root = filepath.Join(ResolvedPath(workRoot), root)
	}
	root = filepath.Clean(root)
	targetCommitted, targetLocal, err := ReadConfigLayers(SDDDir(root))
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
	return fmt.Errorf("%s sets %s, but a repository with project_dir may set only %s; move the rest into the project's own .sdd/ config", path, strings.Join(misplaced, ", "), strings.Join(allowed, ", "))
}

// ResolvedPath resolves symlinks in path, through its nearest existing
// ancestor when path does not exist yet.
func ResolvedPath(path string) string {
	path = filepath.Clean(path)
	var rest []string
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			return filepath.Join(append([]string{resolved}, rest...)...)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return filepath.Join(append([]string{path}, rest...)...)
		}
		rest = append([]string{filepath.Base(path)}, rest...)
		path = parent
	}
}

// RequireProjectApart refuses a project at projectRoot that a work tree at
// workRoot cannot point at: one inside workRepo, the repository holding the
// work tree (empty when there is none), a repository of its own nested there
// included, or one containing the work tree. subject opens the refusal,
// naming where the project came from.
func RequireProjectApart(subject, workRoot, workRepo, projectRoot string) error {
	outer := workRepo
	if outer == "" {
		outer = workRoot
	}
	if WithinDir(outer, projectRoot) {
		return fmt.Errorf("%s lies inside the repository at %s; the project needs a repository of its own", subject, outer)
	}
	if WithinDir(projectRoot, workRoot) {
		return fmt.Errorf("%s contains the work repository at %s; the project needs a repository of its own beside it", subject, workRoot)
	}
	return nil
}

// RequireGraphInProjectRepository refuses a graph_dir, set in sddDir and
// resolved to graphDir, that leads out of the project at projectRoot into no
// repository or into one other than the project's: graph commits, the schema
// stamp and engine branches all belong to the repository holding the
// project's .sdd/. rootOf returns the root of the repository holding a path,
// empty when there is none.
func RequireGraphInProjectRepository(sddDir, projectRoot, graphDir string, rootOf func(string) string) error {
	if WithinDir(projectRoot, graphDir) {
		return nil
	}
	graphRepo := rootOf(graphDir)
	if graphRepo == "" {
		return fmt.Errorf("graph_dir in %s leads to %s, outside the project and outside any repository; keep the graph inside the project", sddDir, graphDir)
	}
	projectRepo := rootOf(projectRoot)
	if projectRepo != "" && SameDir(graphRepo, projectRepo) {
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
