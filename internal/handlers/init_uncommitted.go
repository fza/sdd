package handlers

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

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

// checkInitRecord refuses a record of uncommitted init paths that git could
// commit: it did not come from this checkout's own failed run, and a cloned
// one could sweep untracked files into a pushed commit. Init runs it before
// any write, since a refusal after the writes leaves them uncommitted with no
// record.
func (h *Handler) checkInitRecord(ctx context.Context, repoRoot string) error {
	if h.committer == nil || h.tracker == nil {
		return nil
	}
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return fmt.Errorf("resolving the repository root: %w", err)
	}
	record := uncommittedRecord{repoRoot: repoRoot}
	leftover, err := record.read()
	if err != nil || len(leftover) == 0 {
		return err
	}
	committable, err := h.tracker.Committable(ctx, []string{record.file()})
	if err != nil {
		return fmt.Errorf("checking that git ignores %s: %w", record.file(), err)
	}
	if len(committable) > 0 {
		return fmt.Errorf("%s is tracked by git or not ignored, so init does not trust it: remove it from git or delete it, then run init again", record.file())
	}
	return nil
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
