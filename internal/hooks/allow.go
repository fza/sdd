package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

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
// An unreadable record allows nothing; the returned notice says so and how
// to rewrite it, rather than failing every call that reads the hooks.
func (a *Allows) Allowed(key, hash string) (allowed bool, notice string) {
	record, err := a.load()
	if err != nil {
		return false, fmt.Sprintf("The hook allow record at %s is unreadable (%v), so nothing counts as allowed; `sdd hooks allow` rewrites it.", a.path, err)
	}
	return record[key] == hash, ""
}

// Record sets the repository key's allowed hash. It holds a lock on the
// record across reading and writing it, writes a new file and renames it
// into place, and keeps the record and its folder private to their owner. An
// unreadable record is replaced.
func (a *Allows) Record(key, hash string) error {
	dir := filepath.Dir(a.path)
	err := os.MkdirAll(dir, 0o700)
	if err != nil {
		return err
	}
	err = os.Chmod(dir, 0o700)
	if err != nil {
		return err
	}
	lock, err := os.OpenFile(a.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX)
	if err != nil {
		return fmt.Errorf("locking %s: %w", a.path, err)
	}
	record, err := a.load()
	if err != nil {
		record = map[string]string{}
	}
	record[key] = hash
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomically(a.path, append(encoded, '\n'))
}

// writeAtomically writes data to a new private file beside path and renames
// it over path, so a reader sees the old record or the new one, never part.
func writeAtomically(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, err = file.Write(data)
	if err != nil {
		file.Close()
		return err
	}
	err = file.Chmod(0o600)
	if err != nil {
		file.Close()
		return err
	}
	err = file.Close()
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
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

// Allow records the hooks that came with every repository of the project, as
// they are now, as allowed on this machine, and returns them.
func Allow(ctx context.Context, src Sources, allows *Allows) ([]model.Hook, error) {
	layers, err := declaredLayers(ctx, src)
	if err != nil {
		return nil, err
	}
	var allowed []model.Hook
	for _, group := range gateGroups(layers) {
		hash, err := gateHash(group)
		if err != nil {
			return nil, err
		}
		err = allows.Record(group.gate.key, hash)
		if err != nil {
			return nil, err
		}
		allowed = append(allowed, group.hooks...)
	}
	return allowed, nil
}

// gateGroup is one repository's gated layers, by index into the declared
// layers, and their hooks.
type gateGroup struct {
	gate   *repoGate
	layers []int
	hooks  []model.Hook
}

// gateGroups returns, per repository, the gated layers holding hooks.
func gateGroups(layers []declaredLayer) []gateGroup {
	var groups []gateGroup
	index := map[*repoGate]int{}
	for i, layer := range layers {
		if !layer.gated() || len(layer.hooks) == 0 {
			continue
		}
		at, ok := index[layer.gate]
		if !ok {
			at = len(groups)
			index[layer.gate] = at
			groups = append(groups, gateGroup{gate: layer.gate})
		}
		groups[at].layers = append(groups[at].layers, i)
		groups[at].hooks = append(groups[at].hooks, layer.hooks...)
	}
	return groups
}

// hashedHook is the canonical form of one hook the gate hash covers: its
// config and the digest of every file it names.
type hashedHook struct {
	Layer    model.HookLayer   `json:"layer"`
	Event    string            `json:"event"`
	Command  string            `json:"command"`
	Args     []string          `json:"args"`
	Timeout  int               `json:"timeout"`
	Findings bool              `json:"findings"`
	Files    map[string]string `json:"files"`
}

// gateHash returns the SHA-256 over a repository's gated hooks in order and
// over every file in its hooks folder, local/ included only when the
// repository tracks it. The folder as a whole counts because a hook runs
// more than the script it names: files it sources or imports, files a flag
// names, an interpreter beside it.
func gateHash(group gateGroup) (string, error) {
	hooks, err := hookEntries(group.hooks)
	if err != nil {
		return "", err
	}
	tree, err := treeDigests(group.gate.hooksDir, !group.gate.localGated)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(struct {
		Hooks []hashedHook      `json:"hooks"`
		Tree  map[string]string `json:"tree"`
	}{hooks, tree})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// treeDigests maps every file below dir, by its slash path relative to dir,
// to its content digest and whether it may run. Symbolic links are followed,
// a folder reached twice is recorded rather than walked again, and a broken
// link is recorded by its target. skipLocal leaves out the top-level local/.
func treeDigests(dir string, skipLocal bool) (map[string]string, error) {
	tree := map[string]string{}
	real, err := filepath.EvalSymlinks(dir)
	if errors.Is(err, os.ErrNotExist) {
		return tree, nil
	}
	if err != nil {
		return nil, err
	}
	visited := map[string]bool{real: true}
	var walk func(path, rel string) error
	walk = func(path, rel string) error {
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			child := filepath.Join(path, entry.Name())
			childRel := entry.Name()
			if rel != "" {
				childRel = rel + "/" + entry.Name()
			}
			if skipLocal && childRel == model.HookLocalDir {
				continue
			}
			info, err := os.Stat(child)
			if err != nil {
				target, _ := os.Readlink(child)
				tree[childRel] = "unreadable:" + target
				continue
			}
			switch {
			case info.IsDir():
				realChild, err := filepath.EvalSymlinks(child)
				if err != nil {
					return err
				}
				if visited[realChild] {
					tree[childRel] = "folder seen before:" + realChild
					continue
				}
				visited[realChild] = true
				err = walk(child, childRel)
				if err != nil {
					return err
				}
			case info.Mode().IsRegular():
				digest, err := fileDigest(child)
				if err != nil {
					return err
				}
				tree[childRel] = fmt.Sprintf("%s:%t", digest, info.Mode().Perm()&0o111 != 0)
			default:
				tree[childRel] = info.Mode().String()
			}
		}
		return nil
	}
	err = walk(real, "")
	if err != nil {
		return nil, fmt.Errorf("hashing %s: %w", dir, err)
	}
	return tree, nil
}

// hookEntries returns each hook's config with the digests of its command
// file and of every args entry naming a file, wherever it lies.
func hookEntries(hooks []model.Hook) ([]hashedHook, error) {
	canonical := make([]hashedHook, 0, len(hooks))
	for _, hook := range hooks {
		entry := hashedHook{
			Layer: hook.Origin.Layer, Event: hook.Event, Command: hook.Command, Args: append([]string{}, hook.Args...),
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
				return nil, err
			}
			rel, err := filepath.Rel(hook.Origin.Dir, path)
			if err != nil {
				return nil, err
			}
			entry.Files[filepath.ToSlash(rel)] = digest
		}
		canonical = append(canonical, entry)
	}
	return canonical, nil
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
