package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/networkteam/sdd/internal/model"
)

// Allows records, per repository, the hash of the committed hooks the
// person on this machine allowed to run. A repository's committed hooks
// arrive with a clone, so they run only while their hash matches the record.
type Allows struct {
	path string
}

// NewAllows returns the allow record under stateRoot.
func NewAllows(stateRoot string) *Allows {
	return &Allows{path: filepath.Join(stateRoot, model.HooksDirName, "allowed.json")}
}

// Allowed reports whether hash is the one recorded for the repository key.
func (a *Allows) Allowed(key, hash string) (bool, error) {
	record, err := a.load()
	if err != nil {
		return false, err
	}
	return record[key] == hash, nil
}

// Record sets the repository key's allowed hash.
func (a *Allows) Record(key, hash string) error {
	record, err := a.load()
	if err != nil {
		return err
	}
	record[key] = hash
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	err = os.MkdirAll(filepath.Dir(a.path), 0o700)
	if err != nil {
		return err
	}
	return os.WriteFile(a.path, append(encoded, '\n'), 0o600)
}

func (a *Allows) load() (map[string]string, error) {
	raw, err := os.ReadFile(a.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	record := map[string]string{}
	err = json.Unmarshal(raw, &record)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", a.path, err)
	}
	return record, nil
}

// Allow records the committed hooks every repository of the project declares
// now as allowed on this machine, and returns them.
func Allow(src Sources, allows *Allows) ([]model.Hook, error) {
	layers, err := declaredLayers(src)
	if err != nil {
		return nil, err
	}
	var allowed []model.Hook
	for _, layer := range layers {
		if layer.origin.Layer != model.HookLayerCommitted || len(layer.hooks) == 0 {
			continue
		}
		hash, err := layerHash(layer.hooks)
		if err != nil {
			return nil, err
		}
		err = allows.Record(layer.key, hash)
		if err != nil {
			return nil, err
		}
		allowed = append(allowed, layer.hooks...)
	}
	return allowed, nil
}

// hashedHook is the canonical form of one hook the layer hash covers: its
// config and the digest of every file it names.
type hashedHook struct {
	Event    string            `json:"event"`
	Command  string            `json:"command"`
	Args     []string          `json:"args"`
	Timeout  int               `json:"timeout"`
	Findings bool              `json:"findings"`
	Files    map[string]string `json:"files"`
}

// layerHash returns the SHA-256 over a layer's hooks in order: each hook's
// config and the contents of its command file and of every args entry
// naming a file in its hooks folder.
func layerHash(hooks []model.Hook) (string, error) {
	canonical := make([]hashedHook, 0, len(hooks))
	for _, hook := range hooks {
		entry := hashedHook{
			Event: hook.Event, Command: hook.Command, Args: append([]string{}, hook.Args...),
			Timeout: hook.Timeout, Findings: hook.Findings, Files: map[string]string{},
		}
		paths := []string{hook.Path}
		for _, arg := range hook.Args {
			paths = append(paths, filepath.Join(hook.Origin.Dir, arg))
		}
		for _, path := range paths {
			info, err := os.Stat(path)
			if path == "" || err != nil || !info.Mode().IsRegular() {
				continue
			}
			digest, err := fileDigest(path)
			if err != nil {
				return "", err
			}
			rel, err := filepath.Rel(hook.Origin.Dir, path)
			if err != nil {
				return "", err
			}
			entry.Files[filepath.ToSlash(rel)] = digest
		}
		canonical = append(canonical, entry)
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func fileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	sum := sha256.New()
	_, err = io.Copy(sum, file)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}
