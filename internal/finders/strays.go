package finders

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

// MoveHistory reads the move records a repository's history carries.
type MoveHistory interface {
	MoveRecordTrailers(ctx context.Context, repoDir string) ([]string, error)
}

// GraphStrays reads the move records of the query's repositories and lists
// the entry files their former locations hold on this machine that the
// current graph lacks. A former checkout missing on this machine is skipped:
// no work can strand there.
func (f *Finder) GraphStrays(ctx context.Context, q query.GraphStraysQuery) (*query.GraphStraysResult, error) {
	result := &query.GraphStraysResult{Current: q.Current}
	if f.moveHistory == nil {
		return result, nil
	}
	seen := map[string]bool{}
	for _, repo := range q.Repos {
		if repo == "" || seen[filepath.Clean(repo)] {
			continue
		}
		seen[filepath.Clean(repo)] = true
		blocks, err := f.moveHistory.MoveRecordTrailers(ctx, repo)
		if err != nil {
			return nil, err
		}
		for _, block := range blocks {
			record, found, err := model.ParseMoveRecord(block)
			if err != nil {
				return nil, err
			}
			if found {
				result.Records = append(result.Records, record)
			}
		}
	}
	former := model.FormerLocations(result.Records, q.Current)
	if len(former) == 0 {
		return result, nil
	}
	held := map[string]bool{}
	current, err := entryFiles(q.GraphDir)
	if err != nil {
		return nil, err
	}
	for _, file := range current {
		id, err := model.RelPathToID(file)
		if err == nil {
			held[id] = true
		}
	}
	var folders []model.FormerFolder
	for _, location := range former {
		dir, err := LocationDir(location, q.WorkRoot, q.CloneDir)
		if err != nil {
			return nil, err
		}
		if dir == "" || filepath.Clean(dir) == filepath.Clean(q.GraphDir) {
			continue
		}
		files, err := entryFiles(dir)
		if err != nil {
			return nil, err
		}
		folders = append(folders, model.FormerFolder{Location: location, Dir: filepath.ToSlash(dir), Files: files})
	}
	result.Strays = model.FindStrays(folders, func(id string) bool { return held[id] })
	return result, nil
}

// LocationDir is the graph folder location names on this machine, empty
// when no clone of a git-repo location is configured.
func LocationDir(location model.LocationRef, workRoot string, cloneDir func(model.RepoID) (string, error)) (string, error) {
	var root, dir string
	switch location.Kind {
	case model.LocationGitDir:
		root, dir = workRoot, location.Address
	case model.LocationGitPath:
		root, dir = location.Address, location.Dir()
		if !filepath.IsAbs(root) {
			root = filepath.Join(workRoot, root)
		}
	case model.LocationGitRepo:
		if cloneDir == nil {
			return "", nil
		}
		clone, err := cloneDir(model.RepoID(location.Address))
		if err != nil {
			return "", err
		}
		root, dir = clone, location.Dir()
	default:
		return "", nil
	}
	if filepath.IsAbs(dir) {
		return filepath.Clean(dir), nil
	}
	return filepath.Join(root, dir), nil
}

// entryFiles lists the entry-shaped files under dir, relative to it. A
// folder that does not exist holds none, which also skips a former checkout
// this machine does not hold.
func entryFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if !entry.IsDir() && model.EntryShaped(rel) {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing the entries in %s: %w", dir, err)
	}
	return files, nil
}
