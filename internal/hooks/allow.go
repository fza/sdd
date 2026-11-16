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
	"strings"
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
	_, err = file.Write(data)
	if err == nil {
		err = file.Chmod(0o600)
	}
	err = errors.Join(err, file.Close())
	if err == nil {
		err = os.Rename(file.Name(), path)
	}
	if err != nil {
		return errors.Join(err, os.Remove(file.Name()))
	}
	return nil
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
		hash, err := gateHash(ctx, group)
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

// hashedHook is the canonical form of one hook's declaration the gate hash
// covers.
type hashedHook struct {
	Layer    model.HookLayer `json:"layer"`
	Event    string          `json:"event"`
	Command  string          `json:"command"`
	Args     []string        `json:"args"`
	Timeout  int             `json:"timeout"`
	Findings bool            `json:"findings"`
}

// gateHash returns the SHA-256 over a repository's gated hook declarations
// in order and over every file the repository tracks in its hooks folder,
// by work-tree content and mode, a link by its text. The folder as a whole
// counts because a hook runs more than the script it names: files it
// sources or imports, files a flag names, an interpreter beside it. Only
// tracked files count, since a clone or a pull delivers nothing else, so a
// cache or output a hook writes beside itself leaves the allow standing.
// Links are never followed.
func gateHash(ctx context.Context, group gateGroup) (string, error) {
	hooks := hookEntries(group.hooks)
	tree, err := trackedDigests(ctx, group.gate)
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

// trackedDigests maps every file the repository tracks under its hooks
// folder, by its slash path relative to the repository, to its work-tree
// content digest and whether it may run, a link to its text, a missing file
// to "missing". When the hooks folder is reached through a link, the files
// tracked where it really lies count too; a hooks folder lying outside the
// repository is an error.
func trackedDigests(ctx context.Context, gate *repoGate) (map[string]string, error) {
	if gate.tracker == nil {
		return nil, errors.New("sdd cannot ask git which files the repository tracks")
	}
	paths := []string{relativeTo(gate.root, gate.hooksDir)}
	real, err := filepath.EvalSymlinks(gate.hooksDir)
	if err == nil {
		realRoot, err := filepath.EvalSymlinks(gate.root)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(realRoot, real)
		if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
			return nil, fmt.Errorf("the hooks folder %s lies outside the repository", real)
		}
		if filepath.ToSlash(rel) != paths[0] {
			paths = append(paths, filepath.ToSlash(rel))
		}
	}
	tracked, err := gate.tracker.Tracked(ctx, gate.root, paths...)
	if err != nil {
		return nil, err
	}
	tree := map[string]string{}
	for _, file := range tracked {
		path := filepath.Join(gate.root, filepath.FromSlash(file))
		info, err := os.Lstat(path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			tree[file] = "missing"
		case err != nil:
			return nil, err
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return nil, err
			}
			tree[file] = "link:" + target
		case info.Mode().IsRegular():
			digest, err := fileDigest(path)
			if err != nil {
				return nil, err
			}
			tree[file] = fmt.Sprintf("%s:%t", digest, info.Mode().Perm()&0o111 != 0)
		default:
			tree[file] = info.Mode().String()
		}
	}
	return tree, nil
}

// hookEntries returns each hook's declaration in canonical form.
func hookEntries(hooks []model.Hook) []hashedHook {
	canonical := make([]hashedHook, 0, len(hooks))
	for _, hook := range hooks {
		canonical = append(canonical, hashedHook{
			Layer: hook.Origin.Layer, Event: hook.Event, Command: hook.Command, Args: append([]string{}, hook.Args...),
			Timeout: hook.Timeout, Findings: hook.Findings,
		})
	}
	return canonical
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
