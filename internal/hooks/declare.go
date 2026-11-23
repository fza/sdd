// Package hooks resolves and runs the hooks a project and the person running
// sdd declare: each hook a file in its layer's hooks folder, run without a
// shell. Hooks that came with a repository run only once allowed on this
// machine.
package hooks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/networkteam/sdd/internal/index"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

// Sources names where a project's hooks are declared: the user-global
// config file and the project's repositories. Tracker tells which of a
// repository's machine-local files git tracks; without one, every
// machine-local layer counts as tracked.
type Sources struct {
	GlobalConfig string
	Project      meta.Project
	Tracker      Tracker
}

// Tracker lists the files under paths, relative to root, that the
// repository holding root tracks.
type Tracker interface {
	Tracked(ctx context.Context, root string, paths ...string) ([]string, error)
}

// repoGate is one repository's allow: the layers it covers came with the
// repository. The committed layer always does; the machine-local layer does
// when git tracks its config file or a script under hooks/local/.
type repoGate struct {
	key        string
	root       string
	hooksDir   string
	tracker    Tracker
	localGated bool
	// localWhy is why the local layer is gated when the repository does not
	// simply track it, such as git not answering.
	localWhy string
}

// layerFile is one config file's hooks list; gate is its repository's allow,
// nil for the user-global layer.
type layerFile struct {
	origin model.HookOrigin
	gate   *repoGate
	hooks  []model.HookConfig
}

// declaredLayer is a layerFile with its hooks resolved.
type declaredLayer struct {
	origin model.HookOrigin
	gate   *repoGate
	hooks  []model.Hook
}

// gated reports whether the layer runs only once allowed.
func (l declaredLayer) gated() bool {
	return l.gate != nil && (l.origin.Layer == model.HookLayerCommitted || l.gate.localGated)
}

// declaredLayers reads every layer's hooks, in the order user-global, work
// repository, project repository, committed before local, and resolves each
// command to its file in the layer's hooks folder. A command that does not
// resolve sets the hook's Err rather than failing the whole list, so it
// fails only the calls that reach the hook.
func declaredLayers(ctx context.Context, src Sources) ([]declaredLayer, error) {
	files, err := readLayers(ctx, src)
	if err != nil {
		return nil, err
	}
	layers := make([]declaredLayer, 0, len(files))
	for _, file := range files {
		layer := declaredLayer{origin: file.origin, gate: file.gate}
		for i, config := range file.hooks {
			origin := file.origin
			origin.Index = i
			hook := model.Hook{HookConfig: config, Origin: origin}
			path, err := commandFile(hook)
			if err != nil {
				hook.Err = fmt.Errorf("%s: hooks[%d]: %w", origin.ConfigPath, i, err)
			}
			hook.Path = path
			layer.hooks = append(layer.hooks, hook)
		}
		layers = append(layers, layer)
	}
	return layers, nil
}

func readLayers(ctx context.Context, src Sources) ([]layerFile, error) {
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
	if !project.Pointed() {
		repo, err := repoLayers(ctx, src.Tracker, project.Root, "")
		if err != nil {
			return nil, err
		}
		return append(layers, repo...), nil
	}
	work, err := repoLayers(ctx, src.Tracker, project.WorkRoot, model.HookRepoWork)
	if err != nil {
		return nil, err
	}
	projectLayers, err := repoLayers(ctx, src.Tracker, project.Root, model.HookRepoProject)
	if err != nil {
		return nil, err
	}
	layers = append(layers, work...)
	return append(layers, projectLayers...), nil
}

// repoLayers reads one repository's committed and machine-local layers.
func repoLayers(ctx context.Context, tracker Tracker, root, repo string) ([]layerFile, error) {
	sddDir := meta.SDDDir(root)
	committed, local, err := meta.ReadConfigLayers(sddDir)
	if err != nil {
		return nil, err
	}
	hooksDir := filepath.Join(sddDir, model.HooksDirName)
	var repoID model.RepoID
	if committed != nil {
		repoID = committed.RepoID
	}
	gate := &repoGate{key: index.RepoKey(repoID, root), root: root, hooksDir: hooksDir, tracker: tracker}
	gate.localGated, gate.localWhy = localLayerGated(ctx, tracker, root, sddDir, hooksDir)
	base := model.HookOrigin{Repo: repo, Dir: hooksDir}
	committedLayer := layerFile{origin: base, gate: gate}
	committedLayer.origin.Layer, committedLayer.origin.ConfigPath = model.HookLayerCommitted, filepath.Join(sddDir, "config.yaml")
	if committed != nil {
		committedLayer.hooks = committed.Hooks
	}
	localLayer := layerFile{origin: base, gate: gate}
	localLayer.origin.Layer, localLayer.origin.ConfigPath = model.HookLayerLocal, filepath.Join(sddDir, "config.local.yaml")
	if local != nil {
		localLayer.hooks = local.Hooks
	}
	return []layerFile{committedLayer, localLayer}, nil
}

// localLayerGated reports whether a repository's machine-local layer waits
// for an allow, and why when the reason is not that the repository tracks
// it. It fails closed: the layer is the person's own only when git
// positively reports none of it tracked, matched in any case, and no folder
// on the way to it is a link, which git would not look past.
func localLayerGated(ctx context.Context, tracker Tracker, root, sddDir, hooksDir string) (bool, string) {
	if tracker == nil {
		return true, "sdd cannot ask git which files the repository tracks"
	}
	localDir := filepath.Join(hooksDir, model.HookLocalDir)
	for _, folder := range []string{sddDir, hooksDir, localDir} {
		info, err := os.Lstat(folder)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return true, fmt.Sprintf("%s is a link, past which git does not say what the repository tracks", folder)
		}
	}
	tracked, err := tracker.Tracked(ctx, root, relativeTo(root, filepath.Join(sddDir, "config.local.yaml")), relativeTo(root, localDir))
	if err != nil {
		return true, fmt.Sprintf("git cannot say which files the repository tracks (%v)", err)
	}
	return len(tracked) > 0, ""
}

func relativeTo(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
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
	if hook.Origin.Layer == model.HookLayerCommitted || hook.Origin.Layer == model.HookLayerLocal {
		inLocal, err := withinFolder(path, filepath.Join(hook.Origin.Dir, model.HookLocalDir))
		if err != nil {
			return "", err
		}
		if hook.Origin.Layer == model.HookLayerCommitted && inLocal {
			return "", fmt.Errorf("command %q reaches a script in %s/, which only this machine has; declare the hook in config.local.yaml", hook.Command, model.HookLocalDir)
		}
		if hook.Origin.Layer == model.HookLayerLocal && !inLocal {
			return "", fmt.Errorf("command %q reaches a script outside %s/; a machine-local hook runs only a script of its own there, never one a pull may change", hook.Command, model.HookLocalDir)
		}
	}
	return path, nil
}

// withinFolder reports whether the file path reaches, links resolved, lies
// below folder. It compares folders by identity, not by spelling, so another
// case on a case-insensitive file system or a link changes nothing.
func withinFolder(path, folder string) (bool, error) {
	target, err := os.Stat(folder)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false, err
	}
	for dir := filepath.Dir(real); ; dir = filepath.Dir(dir) {
		info, err := os.Stat(dir)
		if err == nil && os.SameFile(info, target) {
			return true, nil
		}
		if filepath.Dir(dir) == dir {
			return false, nil
		}
	}
}
