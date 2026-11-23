package meta

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/networkteam/sdd/internal/model"
)

// EnsureRuntimeDir creates the runtime folder of the graph in graphDir with
// the .gitignore that keeps it out of git, and returns the folder.
func EnsureRuntimeDir(graphDir string) (string, error) {
	dir := filepath.Join(graphDir, model.GraphRuntimeDir)
	err := os.MkdirAll(dir, 0o755)
	if err != nil {
		return "", fmt.Errorf("creating the graph runtime folder: %w", err)
	}
	return dir, ensureRuntimeIgnore(dir)
}

// KeepRuntimeIgnore writes the .gitignore of the runtime folder in graphDir
// when that folder exists, creating nothing else.
func KeepRuntimeIgnore(graphDir string) error {
	dir := filepath.Join(graphDir, model.GraphRuntimeDir)
	_, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return ensureRuntimeIgnore(dir)
}

func ensureRuntimeIgnore(dir string) error {
	path := filepath.Join(dir, ".gitignore")
	current, err := os.ReadFile(path)
	if err == nil && bytes.Equal(current, []byte(model.GraphRuntimeIgnore)) {
		return nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	err = os.WriteFile(path, []byte(model.GraphRuntimeIgnore), 0o644)
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
