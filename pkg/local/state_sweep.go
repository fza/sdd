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
// category; StateSweep tends it.
const (
	sessionsCategory    = "sessions"
	stagedBlobsCategory = "staged-blobs"
	usedCategory        = "used"
	usedStampSuffix     = ".stamp"
	checkoutsCategory   = "checkouts"
	checkoutsSuffix     = ".list"
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

// RecordCheckout notes checkout as a working tree the project keyed repoKey
// is opened from, so the sweep reads the sessions older versions logged into
// its .sdd/sessions.
func RecordCheckout(stateRoot, repoKey, checkout string) error {
	path, err := checkoutsPath(stateRoot, repoKey)
	if err != nil {
		return err
	}
	recorded, err := readCheckoutRecord(path)
	if err != nil {
		return err
	}
	if slices.Contains(recorded, checkoutEntry{path: checkout}) {
		return nil
	}
	return updateCheckoutRecord(stateRoot, path, func(entries []checkoutEntry) []checkoutEntry {
		i := slices.IndexFunc(entries, func(e checkoutEntry) bool { return e.path == checkout })
		if i < 0 {
			return append(entries, checkoutEntry{path: checkout})
		}
		entries[i].missing = time.Time{}
		return entries
	})
}

func checkoutsPath(stateRoot, repoKey string) (string, error) {
	dir, err := confinedStatePath(stateRoot, checkoutsCategory, repoKey)
	if err != nil {
		return "", err
	}
	return dir + checkoutsSuffix, nil
}

// checkoutEntry is one line of a checkout record: the checkout's path and,
// once a sweep found it gone, a tab and since when (RFC 3339).
type checkoutEntry struct {
	path    string
	missing time.Time
}

func parseCheckoutLine(line string) checkoutEntry {
	if i := strings.LastIndexByte(line, '\t'); i >= 0 {
		if missing, err := time.Parse(time.RFC3339, line[i+1:]); err == nil {
			return checkoutEntry{path: line[:i], missing: missing}
		}
	}
	return checkoutEntry{path: line}
}

func (e checkoutEntry) line() string {
	if e.missing.IsZero() {
		return e.path + "\n"
	}
	return e.path + "\t" + e.missing.UTC().Format(time.RFC3339) + "\n"
}

// readCheckoutRecord reads a checkout record; a missing record names none.
func readCheckoutRecord(path string) ([]checkoutEntry, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading checkout record: %w", err)
	}
	var entries []checkoutEntry
	for line := range strings.Lines(string(content)) {
		line = strings.TrimSuffix(line, "\n")
		if line == "" {
			continue
		}
		entry := parseCheckoutLine(line)
		if !slices.ContainsFunc(entries, func(e checkoutEntry) bool { return e.path == entry.path }) {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// checkoutRecordsLockWait bounds how long a record update waits for another.
const checkoutRecordsLockWait = 5 * time.Second

// updateCheckoutRecord rewrites the record at path with what change makes of
// its entries, under a lock shared by every record of stateRoot; an empty
// result removes the record.
func updateCheckoutRecord(stateRoot, path string, change func([]checkoutEntry) []checkoutEntry) (err error) {
	if err := os.MkdirAll(stateRoot, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", stateRoot, err)
	}
	lock := flock.New(filepath.Join(stateRoot, checkoutsCategory+".lock"))
	ctx, cancel := context.WithTimeout(context.Background(), checkoutRecordsLockWait)
	defer cancel()
	if _, err := lock.TryLockContext(ctx, 20*time.Millisecond); err != nil {
		return fmt.Errorf("locking checkout records: %w", err)
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()
	before, err := readCheckoutRecord(path)
	if err != nil {
		return err
	}
	after := change(slices.Clone(before))
	if slices.Equal(before, after) {
		return nil
	}
	if len(after) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("removing checkout record: %w", err)
		}
		return nil
	}
	var content strings.Builder
	for _, entry := range after {
		content.WriteString(entry.line())
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating checkout records: %w", err)
	}
	temp := path + ".tmp"
	if err := os.WriteFile(temp, []byte(content.String()), 0o644); err != nil {
		return fmt.Errorf("recording checkout: %w", err)
	}
	if err := os.Rename(temp, path); err != nil {
		return fmt.Errorf("recording checkout: %w", err)
	}
	return nil
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
// ended sessions past their retention with their staged attachments, those
// logged in a recorded checkout's .sdd/sessions (RecordCheckout) too, and the
// relevance ledger and empty session folder of a project unused for longer
// than the unused retention. It never removes an open session, a log it
// cannot read, or the staged attachments of either, nor staged attachments
// whose owner may lie in a checkout it cannot read: none recorded, or one
// missing from disk.
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
		missing, err := s.tendCheckoutRecord(key, now, unusedRetention)
		if err != nil {
			errs = append(errs, fmt.Errorf("tending the checkout record of %s: %w", key, err))
			continue
		}
		if !missing {
			orphaned, err := s.removeOrphanedStaged(key, now, unusedRetention, live)
			counts.add(orphaned)
			if err != nil {
				errs = append(errs, fmt.Errorf("removing the orphaned staged attachments of %s: %w", key, err))
			}
		}
		removed, err := s.retireUnused(ctx, key, now, unusedRetention, live)
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

// collect runs session collection over one project's session folder and the
// .sdd/sessions of each checkout recorded for it.
func (s StateSweep) collect(ctx context.Context, key string, now time.Time, retention time.Duration) (StateSwept, error) {
	state := StoreLocation{
		Name: key, Sessions: s.dir(sessionsCategory, key), StagedBlobs: s.dir(stagedBlobsCategory, key),
		Subject: "local", Project: projectOfKey(key),
	}
	counts, err := collectIn(ctx, key, state, []StoreLocation{state}, now, retention)
	if err != nil {
		return counts, err
	}
	path, err := checkoutsPath(s.StateRoot, key)
	if err != nil {
		return counts, err
	}
	checkouts, err := readCheckoutRecord(path)
	if err != nil {
		return counts, err
	}
	for _, checkout := range checkouts {
		if ctx.Err() != nil {
			break
		}
		sddDir := filepath.Join(checkout.path, model.SDDDirName)
		inTree := StoreLocation{
			Name: sddDir, Sessions: filepath.Join(sddDir, sessionsCategory), StagedBlobs: filepath.Join(sddDir, stagedBlobsCategory),
			Subject: "local", Project: "local",
		}
		collected, err := collectIn(ctx, key, inTree, []StoreLocation{state, inTree}, now, retention)
		counts.add(collected)
		if err != nil {
			return counts, fmt.Errorf("collecting the sessions in %s: %w", inTree.Sessions, err)
		}
	}
	return counts, nil
}

// collectIn collects the ended sessions logged under location, with their
// staged attachments in any of staged.
func collectIn(ctx context.Context, key string, location StoreLocation, staged []StoreLocation, now time.Time, retention time.Duration) (StateSwept, error) {
	var counts StateSwept
	if _, err := os.Stat(location.Sessions); errors.Is(err, fs.ErrNotExist) {
		return counts, nil
	}
	sessions, err := NewFilesystemSessionStore(location)
	if err != nil {
		return counts, err
	}
	blobs, err := NewFilesystemStagedBlobStore(staged...)
	if err != nil {
		return counts, err
	}
	measured := []string{location.Sessions}
	for _, l := range staged {
		measured = append(measured, l.StagedBlobs)
	}
	before, err := treeSize(measured...)
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
		after, err := treeSize(measured...)
		if err != nil {
			return counts, err
		}
		counts.Bytes += max(before-after, 0)
	}
	return counts, nil
}

// retireUnused removes the state of a project unused for longer than
// retention: its relevance ledger, its session folder once no session is
// left, and its staged attachments once no session owns them, with its
// checkout record once they are gone.
func (s StateSweep) retireUnused(ctx context.Context, key string, now time.Time, retention time.Duration, live map[app.SessionID]bool) (counts StateSwept, err error) {
	last, err := s.lastUse(key)
	if err != nil || now.Sub(last) <= retention {
		return counts, err
	}
	// A process reopening the project stamps it before it writes there, so
	// each removal reads the stamp again first.
	stillUnused := func() (bool, error) {
		last, err := s.lastUse(key)
		return err == nil && now.Sub(last) > retention, err
	}
	removed := false
	defer func() {
		if removed {
			counts.Projects++
		}
		for _, category := range []string{model.RelevanceDirName, sessionsCategory, stagedBlobsCategory, usedCategory, checkoutsCategory} {
			removeEmptyParents(filepath.Join(s.StateRoot, category), filepath.Dir(s.dir(category, key)))
		}
	}()
	relevance := s.dir(model.RelevanceDirName, key)
	if exists(relevance) {
		size, err := treeSize(relevance)
		if err != nil {
			return counts, err
		}
		if unused, err := stillUnused(); err != nil || !unused {
			return counts, err
		}
		if err := os.RemoveAll(relevance); err != nil {
			return counts, err
		}
		counts.Bytes += size
		removed = true
	}
	sessions := s.dir(sessionsCategory, key)
	gone, err := removeEmptySessionFolder(sessions, stillUnused)
	if err != nil {
		return counts, err
	}
	if !gone && exists(sessions) {
		return counts, nil
	}
	removed = removed || gone
	staged := s.dir(stagedBlobsCategory, key)
	owned, err := s.stagedOwned(ctx, key, staged, live)
	if err != nil {
		return counts, err
	}
	if exists(staged) && !owned {
		size, err := treeSize(staged)
		if err != nil {
			return counts, err
		}
		if unused, err := stillUnused(); err != nil || !unused {
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
	records := []string{stamp}
	if !exists(staged) {
		checkouts, err := checkoutsPath(s.StateRoot, key)
		if err != nil {
			return counts, err
		}
		records = append(records, checkouts)
	}
	for _, record := range records {
		if err := os.Remove(record); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return counts, err
		}
	}
	return counts, nil
}

// stagedOwned reports whether a session may still own staged attachments
// under dir: one whose log lies in the state folder, or one open in a
// checkout recorded for key. With no checkout recorded, or one missing from
// disk, as on an unmounted volume, any owner may be open there.
func (s StateSweep) stagedOwned(ctx context.Context, key, dir string, live map[app.SessionID]bool) (bool, error) {
	owners, err := stagedSessions(dir)
	if err != nil || len(owners) == 0 {
		return false, err
	}
	if slices.ContainsFunc(owners, func(id app.SessionID) bool { return live[id] }) {
		return true, nil
	}
	open, seen, err := s.openInCheckouts(ctx, key)
	if err != nil || !seen {
		return !seen, err
	}
	return slices.ContainsFunc(owners, func(id app.SessionID) bool { return open[id] }), nil
}

// openInCheckouts lists the sessions open in the .sdd/sessions of each
// checkout recorded for key; a log it cannot read counts as open. seen is
// false when no checkout is recorded or one is missing from disk.
func (s StateSweep) openInCheckouts(ctx context.Context, key string) (open map[app.SessionID]bool, seen bool, err error) {
	path, err := checkoutsPath(s.StateRoot, key)
	if err != nil {
		return nil, false, err
	}
	checkouts, err := readCheckoutRecord(path)
	if err != nil || len(checkouts) == 0 {
		return nil, false, err
	}
	open = map[app.SessionID]bool{}
	for _, checkout := range checkouts {
		if _, err := os.Stat(checkout.path); errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		} else if err != nil {
			return nil, false, err
		}
		dir := filepath.Join(checkout.path, model.SDDDirName, sessionsCategory)
		if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, false, err
		}
		sessions, err := NewFilesystemSessionStore(StoreLocation{Name: dir, Sessions: dir, Subject: "local", Project: "local"})
		if err != nil {
			return nil, false, err
		}
		ids, err := app.OpenSessionsIn(ctx, sessions)
		if err != nil {
			return nil, false, fmt.Errorf("listing the sessions in %s: %w", dir, err)
		}
		for _, id := range ids {
			open[id] = true
		}
	}
	return open, true, nil
}

// tendCheckoutRecord notes when a sweep first found a recorded checkout gone
// and drops one gone for longer than retention, as a removed worktree; until
// then it may be an unmounted volume. It reports whether a checkout on record
// is gone. The checkouts are looked up outside the record lock, so a hung
// mount holds no RecordCheckout.
func (s StateSweep) tendCheckoutRecord(key string, now time.Time, retention time.Duration) (missing bool, err error) {
	path, err := checkoutsPath(s.StateRoot, key)
	if err != nil {
		return false, err
	}
	entries, err := readCheckoutRecord(path)
	if err != nil || len(entries) == 0 {
		return false, err
	}
	absent := map[string]bool{}
	for _, entry := range entries {
		_, err := os.Stat(entry.path)
		if errors.Is(err, fs.ErrNotExist) {
			absent[entry.path] = true
		} else if err != nil {
			return false, err
		}
	}
	err = updateCheckoutRecord(s.StateRoot, path, func(entries []checkoutEntry) []checkoutEntry {
		missing = false
		kept := entries[:0]
		for _, entry := range entries {
			switch {
			case !absent[entry.path]:
				entry.missing = time.Time{}
			case entry.missing.IsZero():
				entry.missing = now
			case now.Sub(entry.missing) > retention:
				continue
			}
			missing = missing || absent[entry.path]
			kept = append(kept, entry)
		}
		return kept
	})
	return missing, err
}

// removeOrphanedStaged removes the staged attachments under key of sessions
// no log names any more, in the state folder or a recorded checkout, once
// untouched for longer than retention: those of a removed worktree's sessions,
// which retireUnused never reaches while the project is in use.
func (s StateSweep) removeOrphanedStaged(key string, now time.Time, retention time.Duration, live map[app.SessionID]bool) (counts StateSwept, err error) {
	staged := s.dir(stagedBlobsCategory, key)
	subjects, err := os.ReadDir(staged)
	if errors.Is(err, fs.ErrNotExist) {
		return counts, nil
	}
	if err != nil {
		return counts, err
	}
	logged, err := s.loggedInCheckouts(key)
	if err != nil {
		return counts, err
	}
	for _, subject := range subjects {
		if !subject.IsDir() {
			continue
		}
		sessions, err := os.ReadDir(filepath.Join(staged, subject.Name()))
		if err != nil {
			return counts, err
		}
		for _, session := range sessions {
			id := app.SessionID(session.Name())
			if !session.IsDir() || live[id] || logged[id] {
				continue
			}
			dir := filepath.Join(staged, subject.Name(), session.Name())
			changed, err := lastChange(dir)
			if err != nil {
				return counts, err
			}
			if now.Sub(changed) <= retention {
				continue
			}
			size, err := treeSize(dir)
			if err != nil {
				return counts, err
			}
			if err := os.RemoveAll(dir); err != nil {
				return counts, err
			}
			counts.Bytes += size
		}
	}
	return counts, nil
}

// loggedInCheckouts lists the sessions holding a log, open, ended or
// unreadable, in the .sdd/sessions of a checkout recorded for key.
func (s StateSweep) loggedInCheckouts(key string) (map[app.SessionID]bool, error) {
	path, err := checkoutsPath(s.StateRoot, key)
	if err != nil {
		return nil, err
	}
	checkouts, err := readCheckoutRecord(path)
	if err != nil {
		return nil, err
	}
	logged := map[app.SessionID]bool{}
	for _, checkout := range checkouts {
		entries, err := os.ReadDir(filepath.Join(checkout.path, model.SDDDirName, sessionsCategory))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), sessionLogSuffix) {
				logged[app.SessionID(strings.TrimSuffix(entry.Name(), sessionLogSuffix))] = true
			}
		}
	}
	return logged, nil
}

// lastChange is the latest modification time of dir or anything below it.
func lastChange(dir string) (time.Time, error) {
	var last time.Time
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
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
	return last, err
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
// once no process holds a lock file in it and stillUnused still holds.
func removeEmptySessionFolder(dir string, stillUnused func() (bool, error)) (bool, error) {
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
	if unused, err := stillUnused(); err != nil || !unused {
		return false, err
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
	for category, suffix := range map[string]string{usedCategory: usedStampSuffix, checkoutsCategory: checkoutsSuffix} {
		root := filepath.Join(s.StateRoot, category)
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) && path == root {
					return filepath.SkipAll
				}
				return err
			}
			if d.IsDir() || !strings.HasSuffix(d.Name(), suffix) {
				return nil
			}
			key, err := filepath.Rel(root, strings.TrimSuffix(path, suffix))
			if err != nil {
				return err
			}
			seen[filepath.ToSlash(key)] = true
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("listing %s: %w", root, err)
		}
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
