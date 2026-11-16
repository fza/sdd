// Package hooks resolves, allows and runs the hooks a project and the person
// running sdd declare: each hook a file in its layer's hooks folder, run
// without a shell. A repository's hooks run only while this machine's allow
// record holds their current hash.
package hooks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

// Sources names where a project's hooks are declared: the user-global
// config file and the project's repositories. LocalConfig replaces the
// project's machine-local layer file when set. Tracker tells which files a
// repository tracks; without one no repository's hooks can be allowed.
type Sources struct {
	GlobalConfig string
	Project      meta.Project
	LocalConfig  string
	Tracker      Tracker
}

// Tracker lists the files under paths, relative to root, that the
// repository holding root tracks, matching the paths whatever their case. A
// root git cannot answer for is an error.
type Tracker interface {
	Tracked(ctx context.Context, root string, paths ...string) ([]string, error)
}

// repoGate is one repository's allow: every hook its config files declare,
// keyed by its real root.
type repoGate struct {
	key      string
	root     string
	repo     string
	hooksDir string
	hooks    []model.Hook
}

type layerFile struct {
	origin model.HookOrigin
	gate   *repoGate
	hooks  []model.HookConfig
}

// declare reads every layer's hooks, in the order user-global, work
// repository, project repository, committed before local, resolves each
// command to its file in the layer's hooks folder, and groups the hooks of
// each repository under its gate.
func declare(src Sources) ([]model.Hook, []*repoGate, error) {
	layers, err := readLayers(src)
	if err != nil {
		return nil, nil, err
	}
	var (
		hooks []model.Hook
		gates []*repoGate
	)
	for _, layer := range layers {
		for i, config := range layer.hooks {
			origin := layer.origin
			origin.Index = i
			hook := model.Hook{HookConfig: config, Origin: origin}
			path, err := commandFile(hook)
			if err != nil {
				return nil, nil, fmt.Errorf("%s: hooks[%d]: %w", origin.ConfigPath, i, err)
			}
			hook.Path = path
			hooks = append(hooks, hook)
			if layer.gate == nil {
				continue
			}
			if len(layer.gate.hooks) == 0 {
				gates = append(gates, layer.gate)
			}
			layer.gate.hooks = append(layer.gate.hooks, hook)
		}
	}
	return hooks, gates, nil
}

func readLayers(src Sources) ([]layerFile, error) {
	global, err := repos.LoadConfigFrom(src.GlobalConfig)
	if err != nil {
		return nil, err
	}
	layers := []layerFile{{
		origin: model.HookOrigin{Layer: model.HookLayerGlobal, ConfigPath: src.GlobalConfig, Dir: filepath.Join(filepath.Dir(src.GlobalConfig), model.HooksDirName)},
		hooks:  global.Hooks,
	}}
	project := src.Project
	if project.Root == "" {
		return layers, nil
	}
	// Hooks list per file, so the project's own local file stands apart from
	// the work repository's that lies over it in project_dir layouts.
	localLayer := src.LocalConfig
	if project.GraphRepo != "" {
		localLayer = project.LocalConfigPath(src.LocalConfig)
	}
	projectLocal := meta.LocalConfigPath(project.SDDDir(), localLayer)
	if !project.Pointed() {
		repo, err := repoLayers(project.Root, "", projectLocal)
		if err != nil {
			return nil, err
		}
		return append(layers, repo...), nil
	}
	workLocal := meta.LocalConfigPath(meta.SDDDir(project.WorkRoot), "")
	work, err := repoLayers(project.WorkRoot, model.HookRepoWork, workLocal)
	if err != nil {
		return nil, err
	}
	if projectLocal == workLocal {
		// A graph_repo clone's machine-local layer is the work repository's.
		projectLocal = ""
	}
	projectLayers, err := repoLayers(project.Root, model.HookRepoProject, projectLocal)
	if err != nil {
		return nil, err
	}
	layers = append(layers, work...)
	return append(layers, projectLayers...), nil
}

// repoLayers reads one repository's committed and machine-local layers; an
// empty localPath reads the committed one alone. Both share the
// repository's gate, keyed by its real root: a path a clone cannot choose
// for itself, unlike a repo_id it commits.
func repoLayers(root, repo, localPath string) ([]layerFile, error) {
	sddDir := meta.SDDDir(root)
	committed, local, err := meta.ReadConfigLayers(sddDir, localPath)
	if err != nil {
		return nil, err
	}
	key, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	hooksDir := filepath.Join(sddDir, model.HooksDirName)
	gate := &repoGate{key: key, root: root, repo: repo, hooksDir: hooksDir}
	base := model.HookOrigin{Repo: repo, Dir: hooksDir, Checkout: key}
	committedOrigin := base
	committedOrigin.Layer, committedOrigin.ConfigPath = model.HookLayerCommitted, filepath.Join(sddDir, "config.yaml")
	layers := []layerFile{{origin: committedOrigin, gate: gate}}
	if committed != nil {
		layers[0].hooks = committed.Hooks
	}
	if localPath == "" {
		return layers, nil
	}
	localOrigin := base
	localOrigin.Layer, localOrigin.ConfigPath = model.HookLayerLocal, localPath
	layer := layerFile{origin: localOrigin, gate: gate}
	if local != nil {
		layer.hooks = local.Hooks
	}
	return append(layers, layer), nil
}

// commandFile returns the absolute path of the file the hook's command
// names, refusing a command outside the hooks folder and one that is no
// executable file in it: sdd never looks a command up on PATH.
func commandFile(hook model.Hook) (string, error) {
	rel, err := model.HookCommandPath(hook.Command, hook.Origin.Layer)
	if err != nil {
		return "", err
	}
	path := filepath.Join(hook.Origin.Dir, rel)
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("command %q is not a file in %s; a hook runs a file in its hooks folder and is never looked up on PATH", hook.Command, hook.Origin.Dir)
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("command %q in %s is not a regular file", hook.Command, hook.Origin.Dir)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("command %q in %s is not executable; give it an interpreter shebang and make it executable", hook.Command, hook.Origin.Dir)
	}
	return path, nil
}
