package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

// Store keeps the hook hashes the person on this machine confirmed: one
// file per repository key under <state root>/hooks, so clones of one
// repository share confirmations, safe since a hash pins the content.
type Store struct {
	dir string
	now func() time.Time
}

// NewStore returns the store under stateRoot.
func NewStore(stateRoot string) *Store {
	return &Store{dir: filepath.Join(stateRoot, model.HooksDirName), now: time.Now}
}

// Confirmation is one confirmed hash: when, which hook, and the checkouts
// it was confirmed in.
type Confirmation struct {
	Confirmed time.Time `json:"confirmed"`
	Event     string    `json:"event"`
	Command   string    `json:"command"`
	Checkouts []string  `json:"checkouts"`
}

type storeFile struct {
	Hooks map[string]*Confirmation `json:"hooks"`
}

// Path returns the file holding a repository key's confirmations.
func (s *Store) Path(repoKey string) string {
	return filepath.Join(s.dir, filepath.FromSlash(repoKey)+".json")
}

// Resolve hashes each hook and sets its state: confirmed when its layer
// needs no confirmation or its hash is confirmed, changed when another hash
// of the same event and command was confirmed in its checkout, else pending.
func (s *Store) Resolve(hooks []model.Hook) ([]model.Hook, error) {
	resolved := make([]model.Hook, 0, len(hooks))
	files := map[string]storeFile{}
	for _, hook := range hooks {
		hash, _, err := Hash(hook)
		if err != nil {
			return nil, fmt.Errorf("%s: hooks[%d]: %w", hook.Origin.ConfigPath, hook.Origin.Index, err)
		}
		hook.Hash = hash
		hook.State = model.HookConfirmed
		if hook.Origin.NeedsConfirmation() {
			file, ok := files[hook.Origin.RepoKey]
			if !ok {
				if file, err = s.load(hook.Origin.RepoKey); err != nil {
					return nil, err
				}
				files[hook.Origin.RepoKey] = file
			}
			hook.State = file.state(hook)
		}
		resolved = append(resolved, hook)
	}
	return resolved, nil
}

func (f storeFile) state(hook model.Hook) model.HookState {
	if _, ok := f.Hooks[hook.Hash]; ok {
		return model.HookConfirmed
	}
	for _, confirmation := range f.Hooks {
		if confirmation.Event == hook.Event && confirmation.Command == hook.Command && slices.Contains(confirmation.Checkouts, hook.Origin.Checkout) {
			return model.HookChanged
		}
	}
	return model.HookPending
}

// Confirm records the hook's hash as confirmed in its checkout.
func (s *Store) Confirm(hook model.Hook) error {
	if !hook.Origin.NeedsConfirmation() {
		return nil
	}
	if hook.Hash == "" {
		return fmt.Errorf("%s has no hash to confirm", hook.Label())
	}
	file, err := s.load(hook.Origin.RepoKey)
	if err != nil {
		return err
	}
	confirmation, ok := file.Hooks[hook.Hash]
	if !ok {
		confirmation = &Confirmation{Confirmed: s.now().UTC().Round(time.Second), Event: hook.Event, Command: hook.Command, Checkouts: []string{}}
		file.Hooks[hook.Hash] = confirmation
	}
	if !slices.Contains(confirmation.Checkouts, hook.Origin.Checkout) {
		confirmation.Checkouts = append(confirmation.Checkouts, hook.Origin.Checkout)
	}
	return s.save(hook.Origin.RepoKey, file)
}

// Sweep drops what no checkout needs any more: checkouts gone from disk,
// and hashes no recorded checkout's config still declares. declared returns
// the hashes a checkout's config declares now; a checkout it fails for
// keeps its confirmations, and the failures return joined.
func (s *Store) Sweep(declared func(checkout string) ([]string, error)) error {
	memo := map[string][]string{}
	failed := map[string]bool{}
	var errs []error
	err := filepath.WalkDir(s.dir, func(path string, entry fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && path == s.dir {
			return filepath.SkipDir
		}
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".json" {
			return err
		}
		repoKey := filepath.ToSlash(strings.TrimSuffix(strings.TrimPrefix(path, s.dir+string(filepath.Separator)), ".json"))
		file, err := s.load(repoKey)
		if err != nil {
			errs = append(errs, err)
			return nil
		}
		changed := false
		for hash, confirmation := range file.Hooks {
			var present []string
			keep := false
			for _, checkout := range confirmation.Checkouts {
				if _, err := os.Stat(checkout); errors.Is(err, fs.ErrNotExist) {
					continue
				}
				present = append(present, checkout)
				hashes, ok := memo[checkout]
				if !ok && !failed[checkout] {
					if hashes, err = declared(checkout); err != nil {
						failed[checkout] = true
						errs = append(errs, fmt.Errorf("sweeping hook confirmations of %s: %w", checkout, err))
					} else {
						memo[checkout] = hashes
					}
				}
				keep = keep || failed[checkout] || slices.Contains(hashes, hash)
			}
			switch {
			case !keep:
				delete(file.Hooks, hash)
				changed = true
			case len(present) < len(confirmation.Checkouts):
				confirmation.Checkouts = present
				changed = true
			}
		}
		if !changed {
			return nil
		}
		if len(file.Hooks) == 0 {
			if err := os.Remove(path); err != nil {
				errs = append(errs, err)
			}
			return nil
		}
		if err := s.save(repoKey, file); err != nil {
			errs = append(errs, err)
		}
		return nil
	})
	return errors.Join(append(errs, err)...)
}

func (s *Store) load(repoKey string) (storeFile, error) {
	file := storeFile{Hooks: map[string]*Confirmation{}}
	data, err := os.ReadFile(s.Path(repoKey))
	if errors.Is(err, fs.ErrNotExist) {
		return file, nil
	}
	if err != nil {
		return storeFile{}, err
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return storeFile{}, fmt.Errorf("reading hook confirmations %s: %w", s.Path(repoKey), err)
	}
	if file.Hooks == nil {
		file.Hooks = map[string]*Confirmation{}
	}
	return file, nil
}

func (s *Store) save(repoKey string, file storeFile) error {
	path := s.Path(repoKey)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".confirmations-*")
	if err != nil {
		return err
	}
	_, writeErr := temp.Write(append(data, '\n'))
	closeErr := temp.Close()
	if err := errors.Join(writeErr, closeErr, os.Chmod(temp.Name(), 0o600)); err != nil {
		return errors.Join(err, os.Remove(temp.Name()))
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return errors.Join(err, os.Remove(temp.Name()))
	}
	return nil
}

// DeclaredIn returns the declared function a Sweep asks: the hashes of the
// hooks a checkout's project declares now, graph_repo layouts resolved
// through graphClone.
func DeclaredIn(globalConfig string, graphClone func(model.RepoID) (string, error)) func(checkout string) ([]string, error) {
	return func(checkout string) ([]string, error) {
		project, err := meta.ResolveProject(checkout, graphClone)
		if err != nil {
			return nil, err
		}
		declared, err := Declared(Sources{GlobalConfig: globalConfig, Project: project})
		if err != nil {
			return nil, err
		}
		hashes := make([]string, 0, len(declared))
		for _, hook := range declared {
			hash, _, err := Hash(hook)
			if err != nil {
				return nil, err
			}
			hashes = append(hashes, hash)
		}
		return hashes, nil
	}
}
