package handlers

import (
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
		if line != "" {
			paths = append(paths, filepath.Join(r.repoRoot, filepath.FromSlash(line)))
		}
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
