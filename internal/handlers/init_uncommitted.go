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
// too (20261030-180738-d-tac-f1k).
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

// trustedLeftover narrows the recorded paths to those with a change to
// commit. A record git could commit did not come from this checkout's own
// failed run: a cloned one could sweep untracked files into a pushed commit.
func (h *Handler) trustedLeftover(ctx context.Context, record uncommittedRecord, leftover []string) ([]string, error) {
	if len(leftover) == 0 {
		return nil, nil
	}
	committable, err := h.tracker.Committable(ctx, []string{record.file()})
	if err != nil {
		return nil, fmt.Errorf("checking that git ignores %s: %w", record.file(), err)
	}
	if len(committable) > 0 {
		return nil, fmt.Errorf("%s is tracked by git or not ignored, so init does not trust it: remove it from git or delete it, then run init again", record.file())
	}
	leftover, err = h.tracker.Uncommitted(ctx, leftover)
	if err != nil {
		return nil, fmt.Errorf("selecting the paths an earlier init left uncommitted: %w", err)
	}
	return leftover, nil
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
