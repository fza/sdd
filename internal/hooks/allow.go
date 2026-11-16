package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/networkteam/sdd/internal/model"
)

// Allows is this machine's allow record: per repository, keyed by its real
// root, the hash of its hooks the person allowed to run.
type Allows struct {
	path string
}

// NewAllows returns the allow record under stateRoot.
func NewAllows(stateRoot string) *Allows {
	return &Allows{path: filepath.Join(stateRoot, model.HooksDirName, "allowed.json")}
}

// Path returns the record's file.
func (a *Allows) Path() string { return a.path }

// State returns whether hash is the one recorded for key: allowed, changed
// when another is, else pending. An unreadable record allows nothing; the
// notice says so and how to rewrite it, rather than failing every call that
// reads the hooks.
func (a *Allows) State(key, hash string) (state model.HookState, notice string) {
	record, err := a.load()
	if err != nil {
		return model.HookPending, fmt.Sprintf("the hook allow record at %s is unreadable (%v), so nothing counts as allowed; `sdd hooks allow` rewrites it", a.path, err)
	}
	recorded, ok := record[key]
	switch {
	case !ok:
		return model.HookPending, ""
	case recorded == hash:
		return model.HookAllowed, ""
	default:
		return model.HookChanged, ""
	}
}

// Record sets the allowed hash of each key in hashes. It holds a lock on the
// record across reading and writing it, writes a new file and renames it
// into place, and keeps the record and its folder private to their owner. An
// unreadable record is replaced.
func (a *Allows) Record(hashes map[string]string) error {
	return a.update(func(record map[string]string) bool {
		for key, hash := range hashes {
			record[key] = hash
		}
		return true
	}, true)
}

// Sweep drops the allows of repositories gone from disk. An unreadable
// record is an error and stays as it is.
func (a *Allows) Sweep() error {
	if _, err := os.Stat(a.path); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return a.update(func(record map[string]string) bool {
		changed := false
		for key := range record {
			if _, err := os.Stat(key); errors.Is(err, fs.ErrNotExist) {
				delete(record, key)
				changed = true
			}
		}
		return changed
	}, false)
}

func (a *Allows) update(change func(map[string]string) bool, replaceUnreadable bool) error {
	dir := filepath.Dir(a.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(a.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("locking %s: %w", a.path, err)
	}
	record, err := a.load()
	if err != nil {
		if !replaceUnreadable {
			return err
		}
		record = map[string]string{}
	}
	if !change(record) {
		return nil
	}
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
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	record := map[string]string{}
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, fmt.Errorf("reading %s: %w", a.path, err)
	}
	return record, nil
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

// gateHash returns the SHA-256 over a repository's hook declarations in
// order and over every file the repository tracks in its hooks folder, with
// those files. The folder as a whole counts because a hook runs more than
// the script it names: files it sources or imports, files a flag names, an
// interpreter beside it. Only tracked files count, since a clone or a pull
// delivers nothing else, so a cache or output a hook writes beside itself
// leaves the allow standing.
func gateHash(ctx context.Context, tracker Tracker, gate *repoGate) (string, []model.HookFile, error) {
	files, err := trackedDigests(ctx, tracker, gate)
	if err != nil {
		return "", nil, err
	}
	hooks := make([]hashedHook, 0, len(gate.hooks))
	for _, hook := range gate.hooks {
		hooks = append(hooks, hashedHook{
			Layer: hook.Origin.Layer, Event: hook.Event, Command: hook.Command, Args: append([]string{}, hook.Args...),
			Timeout: hook.Timeout, Findings: hook.Findings,
		})
	}
	encoded, err := json.Marshal(struct {
		Hooks []hashedHook     `json:"hooks"`
		Files []model.HookFile `json:"files"`
	}{hooks, files})
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), files, nil
}

// trackedDigests lists every file the repository tracks under its hooks
// folder, in any case, by work-tree content and whether it may run, a link
// by its text, a missing file as missing. Links are never followed, so a
// link out of the repository costs nothing to hash. When the hooks folder is
// reached through a link, the files tracked where it really lies count too;
// a hooks folder lying outside the repository is an error.
func trackedDigests(ctx context.Context, tracker Tracker, gate *repoGate) ([]model.HookFile, error) {
	if tracker == nil {
		return nil, errors.New("sdd cannot ask git which files the repository tracks")
	}
	paths := []string{relativeTo(gate.root, gate.hooksDir)}
	real, err := filepath.EvalSymlinks(gate.hooksDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		rel, err := filepath.Rel(gate.key, real)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("the hooks folder %s lies outside the repository", real)
		}
		if filepath.ToSlash(rel) != paths[0] {
			paths = append(paths, filepath.ToSlash(rel))
		}
	}
	tracked, err := tracker.Tracked(ctx, gate.root, paths...)
	if err != nil {
		return nil, err
	}
	slices.Sort(tracked)
	tracked = slices.Compact(tracked)
	files := make([]model.HookFile, 0, len(tracked))
	for _, file := range tracked {
		digest, err := trackedDigest(filepath.Join(gate.root, filepath.FromSlash(file)))
		if err != nil {
			return nil, err
		}
		files = append(files, model.HookFile{Path: file, Digest: digest})
	}
	return files, nil
}

func trackedDigest(path string) (string, error) {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "missing", nil
	case err != nil:
		return "", err
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		return "link:" + target, nil
	case info.Mode().IsRegular():
		digest, err := fileDigest(path)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s:%t", digest, info.Mode().Perm()&0o111 != 0), nil
	default:
		return info.Mode().String(), nil
	}
}

func relativeTo(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

func fileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}
