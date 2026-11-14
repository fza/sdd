package repos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"

	"github.com/networkteam/sdd/internal/model"
)

const refMissesFile = "ref-misses.json"

// Head returns the commit the clone's HEAD points at, read from its git
// files.
func (c Clone) Head() (string, error) {
	gitDir := filepath.Join(c.Dir, ".git")
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", fmt.Errorf("reading HEAD of %s: %w", c.name(), err)
	}
	ref, symbolic := strings.CutPrefix(strings.TrimSpace(string(head)), "ref: ")
	if !symbolic {
		return ref, nil
	}
	commit, err := os.ReadFile(filepath.Join(gitDir, filepath.FromSlash(ref)))
	if err == nil {
		return strings.TrimSpace(string(commit)), nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("reading %s of %s: %w", ref, c.name(), err)
	}
	packed, err := os.ReadFile(filepath.Join(gitDir, "packed-refs"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("reading packed refs of %s: %w", c.name(), err)
	}
	for line := range strings.Lines(string(packed)) {
		commit, name, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok && name == ref {
			return commit, nil
		}
	}
	return "", nil
}

// Fetched is when the clone last fetched, read from its FETCH_HEAD, or when
// it was made while it never fetched.
func (c Clone) Fetched() (time.Time, error) {
	for _, name := range []string{"FETCH_HEAD", "HEAD", ""} {
		info, err := os.Stat(filepath.Join(c.Dir, ".git", name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return time.Time{}, fmt.Errorf("reading when %s fetched: %w", c.name(), err)
		}
		return info.ModTime(), nil
	}
	return time.Time{}, nil
}

// Holds reports whether the clone's graph holds the entry id.
func (c Clone) Holds(id string) (bool, error) {
	graphDir, err := c.GraphDir()
	if err != nil {
		return false, err
	}
	rel, err := model.IDToRelPath(id)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(filepath.Join(graphDir, filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("looking for %s in %s: %w", id, c.name(), err)
	}
	return true, nil
}

// RefMisses is the machine state of refs beyond the direct dependencies a
// completed fetch still did not find (20261113-235859-d-cpt-l3m), kept beside
// the git gate's record.
type RefMisses struct {
	dir string
}

// RefMisses returns the record of ref misses under the state root.
func (m *Manager) RefMisses() RefMisses {
	return RefMisses{dir: m.reg.loc.StateRoot}
}

func (s RefMisses) path() string {
	return filepath.Join(s.dir, refMissesFile)
}

// Find returns the recorded miss of entryID in repoID.
func (s RefMisses) Find(ctx context.Context, repoID model.RepoID, entryID string) (model.RefMiss, bool, error) {
	var found model.RefMiss
	var ok bool
	err := s.locked(ctx, true, func(misses []model.RefMiss) ([]model.RefMiss, error) {
		for _, miss := range misses {
			if miss.Repo == repoID && miss.EntryID == entryID {
				found, ok = miss, true
			}
		}
		return nil, nil
	})
	return found, ok, err
}

// Record keeps miss, replacing an earlier one for the same entry.
func (s RefMisses) Record(ctx context.Context, miss model.RefMiss) error {
	return s.locked(ctx, false, func(misses []model.RefMiss) ([]model.RefMiss, error) {
		kept := []model.RefMiss{miss}
		for _, known := range misses {
			if known.Repo != miss.Repo || known.EntryID != miss.EntryID {
				kept = append(kept, known)
			}
		}
		return kept, nil
	})
}

// locked runs change over the recorded misses under the record's lock and
// writes what it returns unless shared.
func (s RefMisses) locked(ctx context.Context, shared bool, change func([]model.RefMiss) ([]model.RefMiss, error)) error {
	if s.dir == "" {
		return errors.New("ref misses: no state root configured")
	}
	err := os.MkdirAll(s.dir, 0o700)
	if err != nil {
		return fmt.Errorf("creating %s: %w", s.dir, err)
	}
	lock := flock.New(s.path() + ".lock")
	take := lock.TryLockContext
	if shared {
		take = lock.TryRLockContext
	}
	_, err = take(ctx, 20*time.Millisecond)
	if err != nil {
		return fmt.Errorf("locking %s: %w", s.path(), err)
	}
	defer func() { _ = lock.Unlock() }()
	var misses []model.RefMiss
	data, err := os.ReadFile(s.path())
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return fmt.Errorf("reading %s: %w", s.path(), err)
	default:
		err = json.Unmarshal(data, &misses)
		if err != nil {
			return fmt.Errorf("reading %s: %w", s.path(), err)
		}
	}
	changed, err := change(misses)
	if err != nil || shared {
		return err
	}
	data, err = json.Marshal(changed)
	if err != nil {
		return fmt.Errorf("encoding %s: %w", s.path(), err)
	}
	temp := s.path() + ".tmp"
	err = os.WriteFile(temp, data, 0o600)
	if err != nil {
		return fmt.Errorf("writing %s: %w", s.path(), err)
	}
	err = os.Rename(temp, s.path())
	if err != nil {
		return fmt.Errorf("writing %s: %w", s.path(), err)
	}
	return nil
}
