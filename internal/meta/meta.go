// Package meta manages the .sdd/ metadata directory: discovery, config
// reading, and path resolution. This is project-level infrastructure,
// separate from the domain model (pure) and graph finders (query-driven).
package meta

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/networkteam/sdd/internal/model"
)

// DiscoverRoot walks up from startDir looking for a directory named ".sdd".
// Returns the repo root (parent of .sdd/) or empty string if not found.
func DiscoverRoot(startDir string) string {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(dir, model.SDDDirName)
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root.
			return ""
		}
		dir = parent
	}
}

// SDDDir returns the absolute path to .sdd/ given the repo root.
func SDDDir(repoRoot string) string {
	return filepath.Join(repoRoot, model.SDDDirName)
}

// LocalLayerFile is the machine-local, gitignored config layer inside the
// .sdd/ directory sddDir.
func LocalLayerFile(sddDir string) string {
	return filepath.Join(sddDir, "config.local.yaml")
}

// LocalLayerOf is localFile, or LocalLayerFile(sddDir) when it is empty.
func LocalLayerOf(sddDir, localFile string) string {
	if localFile != "" {
		return localFile
	}
	return LocalLayerFile(sddDir)
}

// ReadConfig reads and parses .sdd/config.yaml from the given .sdd directory,
// then overlays any .sdd/config.local.yaml present. Returns nil config with
// nil error if neither file exists. The local file is gitignored, parses as
// the same PerRepoConfig schema, and carries genuinely machine-specific
// overrides (API keys, endpoints).
func ReadConfig(sddDir string) (*model.PerRepoConfig, error) {
	return ReadConfigWithLocal(sddDir, LocalLayerFile(sddDir))
}

// ReadConfigWithLocal is ReadConfig with the machine-local layer read from
// localFile: a graph_repo project keeps it in its code repository
// (Project.LocalLayerFile).
func ReadConfigWithLocal(sddDir, localFile string) (*model.PerRepoConfig, error) {
	base, local, err := ReadLayers(sddDir, localFile)
	if err != nil {
		return nil, err
	}
	if base == nil && local == nil {
		return nil, nil
	}
	return model.MergeConfig(base, local), nil
}

// ResolveConfig builds the effective per-repo config from the full overlay:
// the user-global base settings under the committed .sdd/config.yaml under
// .sdd/config.local.yaml. CLI flags apply on top at the call site. Returns
// nil (no config) only when the global base is empty and neither per-repo
// file exists — with global settings present, a repo without config files
// still resolves to them.
func ResolveConfig(global model.BaseConfig, sddDir string) (*model.PerRepoConfig, error) {
	return ResolveConfigWithLocal(global, sddDir, LocalLayerFile(sddDir))
}

// ResolveConfigWithLocal is ResolveConfig with the machine-local layer read
// from localFile.
func ResolveConfigWithLocal(global model.BaseConfig, sddDir, localFile string) (*model.PerRepoConfig, error) {
	committed, local, err := ReadLayers(sddDir, localFile)
	if err != nil {
		return nil, err
	}
	return model.ConfigLayers{Global: global, Project: committed, Local: local}.Effective(), nil
}

// ReadConfigLayers returns the two per-repo config layers separately —
// committed .sdd/config.yaml and machine-local .sdd/config.local.yaml —
// each nil when its file is absent. The effective-config view needs the
// layers for per-value provenance; everything else wants ReadConfig's
// merged result.
func ReadConfigLayers(sddDir string) (committed, local *model.PerRepoConfig, err error) {
	return ReadLayers(sddDir, LocalLayerFile(sddDir))
}

// ReadLayers is ReadConfigLayers with the machine-local layer read from
// localFile, LocalLayerFile(sddDir) when empty. Each layer comes without the
// settings it may not make (model.HonoredCommitted, model.HonoredLocal).
func ReadLayers(sddDir, localFile string) (committed, local *model.PerRepoConfig, err error) {
	if localFile == "" {
		localFile = LocalLayerFile(sddDir)
	}
	committed, err = readConfigFile(filepath.Join(sddDir, "config.yaml"))
	if err != nil {
		return nil, nil, err
	}
	local, err = readConfigFile(localFile)
	if err != nil {
		return nil, nil, err
	}
	return model.HonoredCommitted(committed), model.HonoredLocal(local), nil
}

func readConfigFile(path string) (*model.PerRepoConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return ParseConfigFile(path, data)
}

// ParseConfigFile parses the content read from the per-repo config file at
// path.
func ParseConfigFile(path string, data []byte) (*model.PerRepoConfig, error) {
	cfg, err := model.ParseConfig(data)
	if err != nil {
		// The path names which file carries the offense — the parse error
		// itself already names the key and line.
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// ErrNotAnSDDProject reports a tree without a committed .sdd/config.yaml.
var ErrNotAnSDDProject = errors.New("sdd: no .sdd/config.yaml in the tree")

// ReadCommittedConfigFS reads the committed .sdd/config.yaml from a
// repository root, never the machine-local overlay: what every checkout of
// the repository shares, and all a dependency's cache tells about it.
func ReadCommittedConfigFS(fsys fs.FS) (*model.PerRepoConfig, error) {
	raw, err := fs.ReadFile(fsys, path.Join(model.SDDDirName, "config.yaml"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotAnSDDProject
	}
	if err != nil {
		return nil, err
	}
	cfg, err := model.ParseConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("sdd: .sdd/config.yaml: %w", err)
	}
	return cfg, nil
}

// ResolveGraphDir returns the absolute graph directory path from a config
// and the repo root (parent of .sdd/). If cfg is nil or GraphDir is empty,
// falls back to model.DefaultGraphDir.
func ResolveGraphDir(repoRoot string, cfg *model.PerRepoConfig) string {
	graphDir := model.DefaultGraphDir
	if cfg != nil && cfg.GraphDir != "" {
		graphDir = cfg.GraphDir
	}
	if filepath.IsAbs(graphDir) {
		return graphDir
	}
	return filepath.Join(repoRoot, graphDir)
}

// TmpDir returns the path to .sdd/tmp/ given the .sdd/ directory path.
func TmpDir(sddDir string) string {
	return filepath.Join(sddDir, "tmp")
}

// IsSDDMetaDir returns true if the directory entry is the .sdd metadata
// directory. Used by graph scanning to skip .sdd/ contents.
func IsSDDMetaDir(d fs.DirEntry) bool {
	return d.IsDir() && d.Name() == model.SDDDirName
}
