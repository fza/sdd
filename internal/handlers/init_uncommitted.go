package handlers

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

// trustedRecord reads the paths an earlier run's failed commit left behind.
// Init calls it before any write: the record decides what the commit takes,
// and a refusal after the writes would leave them uncommitted with no record.
// A record git does not ignore did not come from this checkout's own failed
// run: a cloned one could sweep untracked files into a pushed commit.
func (h *Handler) trustedRecord(ctx context.Context, repoRoot string) (uncommittedRecord, []string, error) {
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return uncommittedRecord{}, nil, fmt.Errorf("resolving the repository root: %w", err)
	}
	record := uncommittedRecord{repoRoot: repoRoot}
	leftover, err := record.read()
	if err != nil {
		return record, nil, err
	}
	if len(leftover) == 0 || h.tracker == nil {
		return record, leftover, nil
	}
	ignored, err := h.tracker.Ignored(ctx, record.file())
	if err != nil {
		return record, nil, fmt.Errorf("checking that git ignores %s: %w", record.file(), err)
	}
	if !ignored {
		return record, nil, fmt.Errorf("%s is tracked by git or not ignored, so init does not trust it: remove it from git or delete it, then run init again", record.file())
	}
	return record, leftover, nil
}

// commitInit commits the paths init touched together with those an earlier
// run's failed commit left behind, limited to paths git reports a change
// under. A failed commit records its paths for the next run.
func (h *Handler) commitInit(ctx context.Context, record uncommittedRecord, leftover, touched []string, message string) error {
	commitPaths := filterRepoPaths(record.repoRoot, touched)
	if h.tracker != nil {
		var err error
		commitPaths, err = h.tracker.Uncommitted(ctx, commitPaths)
		if err != nil {
			return fmt.Errorf("selecting init paths that differ from HEAD: %w", err)
		}
		leftover, err = h.tracker.Uncommitted(ctx, leftover)
		if err != nil {
			return fmt.Errorf("selecting the paths an earlier init left uncommitted: %w", err)
		}
	}
	for _, path := range leftover {
		if !slices.Contains(commitPaths, path) {
			commitPaths = append(commitPaths, path)
		}
	}
	if len(commitPaths) == 0 {
		return record.clear()
	}
	err := h.committer.Commit(message, commitPaths...)
	if err != nil {
		return errors.Join(fmt.Errorf("git commit, which the next sdd init retries: %w", err), record.write(commitPaths))
	}
	return record.clear()
}

// uncommittedRecord lists, relative to a repository root, the paths an init
// commit holds until it lands, so a run after a failed commit commits them
// too.
type uncommittedRecord struct {
	repoRoot string
}

func (r uncommittedRecord) file() string {
	return filepath.Join(meta.TmpDir(filepath.Join(r.repoRoot, model.SDDDirName)), "init-uncommitted")
}

func (r uncommittedRecord) read() ([]string, error) {
	content, err := os.ReadFile(r.file())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the paths an earlier init left uncommitted: %w", err)
	}
	var paths []string
	for line := range strings.Lines(string(content)) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		rel := filepath.FromSlash(line)
		if !filepath.IsLocal(rel) || filepath.Clean(rel) != rel || rel == "." {
			return nil, fmt.Errorf("%s names %q, which is no file path inside the repository: delete the record and run init again", r.file(), line)
		}
		paths = append(paths, filepath.Join(r.repoRoot, rel))
	}
	return paths, nil
}

func (r uncommittedRecord) write(paths []string) error {
	var content strings.Builder
	for _, path := range paths {
		rel, err := filepath.Rel(r.repoRoot, path)
		if err != nil {
			return fmt.Errorf("recording %s: %w", path, err)
		}
		content.WriteString(filepath.ToSlash(rel) + "\n")
	}
	err := os.MkdirAll(filepath.Dir(r.file()), 0o755)
	if err == nil {
		err = os.WriteFile(r.file(), []byte(content.String()), 0o644)
	}
	if err != nil {
		return fmt.Errorf("recording the paths init commits: %w", err)
	}
	return nil
}

func (r uncommittedRecord) clear() error {
	err := os.Remove(r.file())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("removing the record of uncommitted init paths: %w", err)
	}
	return nil
}
