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
	localPath := LocalConfigPath(sddDir, "")
	committed, local, err := ReadConfigLayers(sddDir, localPath)
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
