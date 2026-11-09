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

	"github.com/networkteam/sdd/internal/segment"
)

// writeSegmentFile writes data as a fresh segment in dir: a temporary file
// renamed into place, so a reader never sees a partial segment.
func writeSegmentFile(dir string, data []byte) (err error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	name, err := segment.Name(time.Now())
	if err != nil {
		return err
	}
	temp := "." + name + ".tmp"
	file, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err == nil {
		err = root.Rename(temp, name)
	}
	if err != nil {
		return errors.Join(err, root.Remove(temp))
	}
	return nil
}

type segmentFile struct {
	path string
	name string
	data []byte
}

// readSegmentFiles returns the segments of dir in name order, none when dir
// does not exist. A segment a compaction removes between the listing and
// the read restarts the listing, which then holds the merged segment the
// compaction wrote first.
func readSegmentFiles(ctx context.Context, dir string) ([]segmentFile, error) {
	for range maxSegmentListings {
		files, vanished, err := listSegmentFiles(ctx, dir)
		if err != nil || !vanished {
			return files, err
		}
	}
	return nil, fmt.Errorf("%s: segments kept vanishing while read", dir)
}

const maxSegmentListings = 8

func listSegmentFiles(ctx context.Context, dir string) ([]segmentFile, bool, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var files []segmentFile
	for _, entry := range entries {
		if entry.IsDir() || !segment.IsName(entry.Name()) {
			continue
		}
		err := ctx.Err()
		if err != nil {
			return nil, false, err
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, true, nil
		}
		if err != nil {
			return nil, false, err
		}
		files = append(files, segmentFile{path: path, name: entry.Name(), data: data})
	}
	slices.SortFunc(files, func(a, b segmentFile) int { return strings.Compare(a.name, b.name) })
	return files, false, nil
}
