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

	"github.com/networkteam/slogutils"

	app "github.com/networkteam/sdd/pkg/application"
)

// The state root keeps per-project state under one repository key
// (index.RepoKey) per category; StateSweep tends it.
const (
	sessionsCategory    = "sessions"
	stagedBlobsCategory = "staged-blobs"
	collectPageSize     = 200
)

// StateSwept is what one StateSweep pass removed. Unreadable counts the
// session logs it kept because it could not read them.
type StateSwept struct {
	Sessions   int
	Unreadable int
	Bytes      int64
}

func (s *StateSwept) add(o StateSwept) {
	s.Sessions += o.Sessions
	s.Unreadable += o.Unreadable
	s.Bytes += o.Bytes
}

// StateSweep removes the per-project state under StateRoot nothing needs:
// the ended sessions of every project past their retention, with their staged
// attachments. It never removes an open session, a log it cannot read, or the
// staged attachments of either.
type StateSweep struct {
	StateRoot string
}

// Sweep runs one pass and reports what it removed. unusedRetention is how
// long an unused project's state stays.
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

// projectKeys lists the repository keys holding a session folder.
func (s StateSweep) projectKeys() ([]string, error) {
	seen := map[string]bool{}
	root := filepath.Join(s.StateRoot, sessionsCategory)
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

// projectOfKey is the project a log under key is attributed to, as
// SessionLocations attributes the location of that key.
func projectOfKey(key string) app.ProjectID {
	if strings.HasPrefix(key, "local/") {
		return "local"
	}
	return app.ProjectID(key)
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
