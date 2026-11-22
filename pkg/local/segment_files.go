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

// logFiles names the files of an append-log directory: a new file gets the
// extension write, and a read takes every extension in read.
type logFiles struct {
	write string
	read  []string
}

var segmentLog = logFiles{write: segment.Ext, read: []string{segment.Ext}}

// writeSegmentFile writes data as a fresh segment in dir.
func writeSegmentFile(dir string, data []byte) error {
	return writeLogFile(dir, segment.Ext, data)
}

// writeLogFile writes data as a fresh file <time>-<random><ext> in dir: a
// temporary file renamed into place, so a reader never sees a partial file.
func writeLogFile(dir, ext string, data []byte) (err error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	name, err := segment.Name(time.Now())
	if err != nil {
		return err
	}
	name = strings.TrimSuffix(name, segment.Ext) + ext
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

type logFile struct {
	path string
	name string
	data []byte
}

// readSegmentFiles returns the segments of dir in name order.
func readSegmentFiles(ctx context.Context, dir string) ([]logFile, error) {
	return readLogFiles(ctx, dir, segmentLog.read)
}

// readLogFiles returns the files of dir carrying one of exts, in name order,
// none when dir does not exist. A file a compaction removes between the
// listing and the read restarts the listing, which then holds the merged
// file the compaction wrote first.
func readLogFiles(ctx context.Context, dir string, exts []string) ([]logFile, error) {
	for range maxSegmentListings {
		files, vanished, err := listLogFiles(ctx, dir, exts)
		if err != nil || !vanished {
			return files, err
		}
	}
	return nil, fmt.Errorf("%s: segments kept vanishing while read", dir)
}

const maxSegmentListings = 8

func listLogFiles(ctx context.Context, dir string, exts []string) ([]logFile, bool, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var files []logFile
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || !slices.Contains(exts, filepath.Ext(entry.Name())) {
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
		files = append(files, logFile{path: path, name: entry.Name(), data: data})
	}
	slices.SortFunc(files, func(a, b logFile) int { return strings.Compare(a.name, b.name) })
	return files, false, nil
}
