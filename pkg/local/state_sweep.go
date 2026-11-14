package local

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/model"
	app "github.com/networkteam/sdd/pkg/application"
)

// The state root keeps per-project state under one repository key per
// category; StateSweep tends it (20261114-095140-d-cpt-ad4, rules 2 and 3).
const (
	sessionsCategory    = "sessions"
	stagedBlobsCategory = "staged-blobs"
	usedCategory        = "used"
	usedStampSuffix     = ".stamp"
	// projectUseRefresh is how stale a project's use stamp may grow before a
	// use rewrites it.
	projectUseRefresh = 24 * time.Hour
	collectPageSize   = 200
)

// MarkProjectUsed stamps the project keyed repoKey (index.RepoKey) as used at
// now, rewriting the stamp at most once a day.
func MarkProjectUsed(stateRoot, repoKey string, now time.Time) error {
	path, err := projectStampPath(stateRoot, repoKey)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	switch {
	case err == nil:
		if now.Sub(info.ModTime()) < projectUseRefresh {
			return nil
		}
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("creating project use stamps: %w", err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			return fmt.Errorf("creating project use stamp: %w", err)
		}
	default:
		return fmt.Errorf("reading project use stamp: %w", err)
	}
	if err := os.Chtimes(path, now, now); err != nil {
		return fmt.Errorf("refreshing project use stamp: %w", err)
	}
	return nil
}

func projectStampPath(stateRoot, repoKey string) (string, error) {
	dir, err := confinedStatePath(stateRoot, usedCategory, repoKey)
	if err != nil {
		return "", err
	}
	return dir + usedStampSuffix, nil
}

// StateSwept is what one StateSweep pass removed. Unreadable counts the
// session logs it kept because it could not read them.
type StateSwept struct {
	Sessions   int
	Projects   int
	Unreadable int
	Bytes      int64
}

func (s *StateSwept) add(o StateSwept) {
	s.Sessions += o.Sessions
	s.Projects += o.Projects
	s.Unreadable += o.Unreadable
	s.Bytes += o.Bytes
}

// StateSweep removes the per-project state under StateRoot nothing needs:
// ended sessions past their retention with their staged attachments, and the
// relevance ledger and empty session folder of a project unused for longer
// than the unused retention. It never removes an open session or a log it
// cannot read.
type StateSweep struct {
	StateRoot string
}

// Sweep runs one pass and reports what it removed.
func (s StateSweep) Sweep(ctx context.Context, now time.Time, sessionRetention, unusedRetention time.Duration) (StateSwept, error) {
	var counts StateSwept
	keys, err := s.projectKeys()
	if err != nil {
		return counts, err
	}
	var errs []error
	for _, key := range keys {
		if ctx.Err() != nil {
			break
		}
		collected, err := s.collect(ctx, key, now, sessionRetention)
		counts.add(collected)
		if err != nil {
			errs = append(errs, fmt.Errorf("collecting the sessions of %s: %w", key, err))
		}
	}
	live, err := s.sessionIDs()
	if err != nil {
		return counts, errors.Join(append(errs, err)...)
	}
	for _, key := range keys {
		if ctx.Err() != nil {
			break
		}
		removed, err := s.retireUnused(key, now, unusedRetention, live)
		counts.add(removed)
		if err != nil {
			errs = append(errs, fmt.Errorf("removing the state of unused project %s: %w", key, err))
		}
	}
	return counts, errors.Join(errs...)
}

func (s StateSweep) dir(category, key string) string {
	return filepath.Join(s.StateRoot, category, filepath.FromSlash(key))
}

// collect runs session collection over one project's session folder.
func (s StateSweep) collect(ctx context.Context, key string, now time.Time, retention time.Duration) (StateSwept, error) {
	var counts StateSwept
	location := StoreLocation{
		Name: key, Sessions: s.dir(sessionsCategory, key), StagedBlobs: s.dir(stagedBlobsCategory, key),
		Subject: "local", Project: projectOfKey(key),
	}
	if _, err := os.Stat(location.Sessions); errors.Is(err, fs.ErrNotExist) {
		return counts, nil
	}
	sessions, err := NewFilesystemSessionStore(location)
	if err != nil {
		return counts, err
	}
	blobs, err := NewFilesystemStagedBlobStore(location)
	if err != nil {
		return counts, err
	}
	before, err := treeSize(location.Sessions, location.StagedBlobs)
	if err != nil {
		return counts, err
	}
	cmd := app.CollectSessionsCmd{Retention: retention, Limit: collectPageSize}
	for {
		result, err := app.CollectSessionsIn(ctx, sessions, blobs, now, cmd)
		counts.Sessions += len(result.RemovedSessions)
		for _, id := range result.Skipped {
			slogutils.FromContext(ctx).Warn("kept a session log sdd cannot read", "project", key, "session", id)
		}
		counts.Unreadable += len(result.Skipped)
		if err != nil {
			return counts, err
		}
		if result.Next == "" || ctx.Err() != nil {
			break
		}
		cmd.After = result.Next
	}
	if counts.Sessions > 0 {
		after, err := treeSize(location.Sessions, location.StagedBlobs)
		if err != nil {
			return counts, err
		}
		counts.Bytes += max(before-after, 0)
	}
	return counts, nil
}

// retireUnused removes the state of a project unused for longer than
// retention: its relevance ledger, its session folder once no session is
// left, and its staged attachments once no session anywhere owns them.
func (s StateSweep) retireUnused(key string, now time.Time, retention time.Duration, live map[app.SessionID]bool) (counts StateSwept, err error) {
	last, err := s.lastUse(key)
	if err != nil || now.Sub(last) <= retention {
		return counts, err
	}
	removed := false
	defer func() {
		if removed {
			counts.Projects++
		}
		for _, category := range []string{model.RelevanceDirName, sessionsCategory, stagedBlobsCategory, usedCategory} {
			removeEmptyParents(filepath.Join(s.StateRoot, category), filepath.Dir(s.dir(category, key)))
		}
	}()
	relevance := s.dir(model.RelevanceDirName, key)
	if exists(relevance) {
		size, err := treeSize(relevance)
		if err != nil {
			return counts, err
		}
		if err := os.RemoveAll(relevance); err != nil {
			return counts, err
		}
		counts.Bytes += size
		removed = true
	}
	sessions := s.dir(sessionsCategory, key)
	gone, err := removeEmptySessionFolder(sessions)
	if err != nil {
		return counts, err
	}
	if !gone && exists(sessions) {
		return counts, nil
	}
	removed = removed || gone
	staged := s.dir(stagedBlobsCategory, key)
	owned, err := stagedSessions(staged)
	if err != nil {
		return counts, err
	}
	if exists(staged) && !slices.ContainsFunc(owned, func(id app.SessionID) bool { return live[id] }) {
		size, err := treeSize(staged)
		if err != nil {
			return counts, err
		}
		if err := os.RemoveAll(staged); err != nil {
			return counts, err
		}
		counts.Bytes += size
		removed = true
	}
	stamp, err := projectStampPath(s.StateRoot, key)
	if err != nil {
		return counts, err
	}
	if err := os.Remove(stamp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return counts, err
	}
	return counts, nil
}

// lastUse is the project's use stamp, or for a project no binary stamped yet
// the latest change in its relevance ledger and session folder.
func (s StateSweep) lastUse(key string) (time.Time, error) {
	stamp, err := projectStampPath(s.StateRoot, key)
	if err != nil {
		return time.Time{}, err
	}
	if info, err := os.Stat(stamp); err == nil {
		return info.ModTime(), nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return time.Time{}, err
	}
	var last time.Time
	for _, dir := range []string{s.dir(model.RelevanceDirName, key), s.dir(sessionsCategory, key)} {
		err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if info.ModTime().After(last) {
				last = info.ModTime()
			}
			return nil
		})
		if err != nil {
			return time.Time{}, err
		}
	}
	return last, nil
}

// removeEmptySessionFolder removes a session folder holding no session log,
// once no process holds a lock file in it.
func removeEmptySessionFolder(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), sessionLogSuffix) || entry.IsDir() {
			return false, nil
		}
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".lock") {
			continue
		}
		lock := flock.New(filepath.Join(dir, entry.Name()))
		taken, err := lock.TryLock()
		if err != nil {
			return false, err
		}
		if !taken {
			return false, nil
		}
		defer func() { _ = lock.Unlock() }()
	}
	if err := os.RemoveAll(dir); err != nil {
		return false, err
	}
	return true, nil
}

// projectKeys lists the repository keys holding state in any category.
func (s StateSweep) projectKeys() ([]string, error) {
	seen := map[string]bool{}
	for _, category := range []string{sessionsCategory, model.RelevanceDirName} {
		root := filepath.Join(s.StateRoot, category)
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) && path == root {
					return filepath.SkipAll
				}
				return err
			}
			if !d.IsDir() || path == root {
				return nil
			}
			leaf, err := holdsState(path)
			if err != nil || !leaf {
				return err
			}
			key, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			seen[filepath.ToSlash(key)] = true
			return filepath.SkipDir
		})
		if err != nil {
			return nil, fmt.Errorf("listing %s: %w", root, err)
		}
	}
	root := filepath.Join(s.StateRoot, usedCategory)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && path == root {
				return filepath.SkipAll
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), usedStampSuffix) {
			return nil
		}
		key, err := filepath.Rel(root, strings.TrimSuffix(path, usedStampSuffix))
		if err != nil {
			return err
		}
		seen[filepath.ToSlash(key)] = true
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", root, err)
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys, nil
}

// holdsState reports whether dir is a project's own folder: it holds a file
// or nothing at all, where a folder above the keys holds only folders.
func holdsState(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if !entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			return true, nil
		}
	}
	return !slices.ContainsFunc(entries, fs.DirEntry.IsDir), nil
}

// sessionIDs lists every session log under the state root.
func (s StateSweep) sessionIDs() (map[app.SessionID]bool, error) {
	ids := map[app.SessionID]bool{}
	root := filepath.Join(s.StateRoot, sessionsCategory)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && path == root {
				return filepath.SkipAll
			}
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), sessionLogSuffix) {
			ids[app.SessionID(strings.TrimSuffix(d.Name(), sessionLogSuffix))] = true
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", root, err)
	}
	return ids, nil
}

// stagedSessions lists the sessions a staged-blobs folder holds attachments
// of: <subject>/<session>/ below it.
func stagedSessions(dir string) ([]app.SessionID, error) {
	subjects, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []app.SessionID
	for _, subject := range subjects {
		if !subject.IsDir() {
			continue
		}
		sessions, err := os.ReadDir(filepath.Join(dir, subject.Name()))
		if err != nil {
			return nil, err
		}
		for _, session := range sessions {
			if session.IsDir() {
				ids = append(ids, app.SessionID(session.Name()))
			}
		}
	}
	return ids, nil
}

func projectOfKey(key string) app.ProjectID {
	if strings.HasPrefix(key, "local/") {
		return "local"
	}
	return app.ProjectID(key)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// treeSize sums the sizes of the files under dirs; a missing one counts zero.
func treeSize(dirs ...string) (int64, error) {
	var total int64
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			if err != nil || d.IsDir() {
				return err
			}
			info, err := d.Info()
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			total += info.Size()
			return nil
		})
		if err != nil {
			return 0, fmt.Errorf("measuring %s: %w", dir, err)
		}
	}
	return total, nil
}

// removeEmptyParents removes dir and its parents up to, not including, root
// while they are empty.
func removeEmptyParents(root, dir string) {
	for dir != root && strings.HasPrefix(dir, root+string(filepath.Separator)) {
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}
