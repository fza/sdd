// Package hooks resolves, confirms and runs the hooks a project and the
// person running sdd declare (20261109-213250-d-tac-bve): each hook a file in
// its layer's hooks folder, hashed over its config and the files it names,
// run without a shell once its hash is confirmed.
package hooks

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/networkteam/sdd/internal/index"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

// Sources names where a project's hooks are declared: the user-global
// config file and the project's repositories. LocalConfig replaces the
// project's machine-local layer file when set.
type Sources struct {
	GlobalConfig string
	Project      meta.Project
	LocalConfig  string
}

type layerFile struct {
	origin model.HookOrigin
	hooks  []model.HookConfig
}

// Declared reads every layer's hooks, in the order user-global, work
// repository, project repository, committed before local, and resolves each
// command to its file in the layer's hooks folder. The hooks carry neither
// hash nor state.
func Declared(src Sources) ([]model.Hook, error) {
	layers, err := readLayers(src)
	if err != nil {
		return nil, err
	}
	var hooks []model.Hook
	for _, layer := range layers {
		for i, config := range layer.hooks {
			origin := layer.origin
			origin.Index = i
			hook := model.Hook{HookConfig: config, Origin: origin}
			path, err := commandFile(hook)
			if err != nil {
				return nil, fmt.Errorf("%s: hooks[%d]: %w", origin.ConfigPath, i, err)
			}
			hook.Path = path
			hooks = append(hooks, hook)
		}
	}
	return hooks, nil
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
	projectLocal := meta.LocalConfigPath(project.SDDDir(), project.LocalConfigPath(src.LocalConfig))
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
// empty localPath reads the committed one alone.
func repoLayers(root, repo, localPath string) ([]layerFile, error) {
	sddDir := meta.SDDDir(root)
	committed, local, err := meta.ReadConfigLayers(sddDir, localPath)
	if err != nil {
		return nil, err
	}
	var repoID model.RepoID
	if committed != nil {
		repoID = committed.RepoID
	}
	base := model.HookOrigin{Repo: repo, Dir: filepath.Join(sddDir, model.HooksDirName), Checkout: root, RepoKey: index.RepoKey(repoID, root)}
	committedOrigin := base
	committedOrigin.Layer, committedOrigin.ConfigPath = model.HookLayerCommitted, filepath.Join(sddDir, "config.yaml")
	layers := []layerFile{{origin: committedOrigin}}
	if committed != nil {
		layers[0].hooks = committed.Hooks
	}
	if localPath == "" {
		return layers, nil
	}
	localOrigin := base
	localOrigin.Layer, localOrigin.ConfigPath = model.HookLayerLocal, localPath
	layer := layerFile{origin: localOrigin}
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
