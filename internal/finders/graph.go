package finders

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/networkteam/sdd/internal/filestamp"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

// filesystemSource is the DocumentSource over a graph directory (hierarchical
// YYYY/MM/ layout). It hands raw canonical content — the CLI path does no
// double YAML parse, so it stays byte-identical to the direct on-disk read —
// and scans co-located attachment directories. It deliberately does not read
// wip/: the CLI resolves WIP markers lazily (GraphFinder.WIPMarkers) only when
// a layout needs them, so a plain graph load never touches or fails on them.
type filesystemSource struct {
	dir   string
	cache GraphCache
}

// GraphCache keeps a graph directory's raw documents between processes. Load
// answers only for exactly the file stamps the documents were saved under;
// any other stamps, and any cache it cannot verify, are a miss. The documents
// still pass the single ParseEntry gate on every load, so a hit hands
// buildGraph the same bytes a fresh read would.
type GraphCache interface {
	Load(dir string, stamps map[string]filestamp.Stamp) (GraphDocuments, bool, error)
	Save(dir string, stamps map[string]filestamp.Stamp, docs GraphDocuments) error
}

// runtimeDirName holds a graph store's locks and lineage. Its files never
// become entries, so they do not count toward the graph's stamps.
const runtimeDirName = ".sdd-runtime"

// GraphDocuments walks the graph directory and reads every entry file as raw
// canonical content. File-read and walk errors are hard failures; files that
// do not match the ID layout are skipped (not the graph's concern). A file
// whose content fails to parse is not detected here — the raw bytes flow to
// buildGraph, whose single ParseEntry gate records the failure as a LoadIssue.
// Files are read concurrently: each costs an open and a close, which dominate
// a graph of thousands of small files.
func (s filesystemSource) GraphDocuments() (GraphDocuments, error) {
	var docs GraphDocuments
	var paths []string
	dirs := map[string]bool{}
	now := time.Now()
	stamps := map[string]filestamp.Stamp{}
	proven := s.cache != nil
	walkErr := filepath.WalkDir(s.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if model.IsWIPDir(d) {
			return fs.SkipDir
		}
		if meta.IsSDDMetaDir(d) {
			return fs.SkipDir
		}
		rel, err := filepath.Rel(s.dir, path)
		if err != nil {
			return fmt.Errorf("getting relative path for %s: %w", path, err)
		}
		if d.IsDir() {
			dirs[rel] = true
			return nil
		}
		if proven && !strings.HasPrefix(rel, runtimeDirName+string(filepath.Separator)) {
			info, err := os.Lstat(path)
			if err != nil {
				return fmt.Errorf("stamping %s: %w", path, err)
			}
			stamp := filestamp.Of(info)
			// A symlink's target can change without its stamp changing.
			if !stamp.Proves(now) || info.Mode()&fs.ModeSymlink != 0 {
				proven = false
			}
			stamps[rel] = stamp
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		id, err := model.RelPathToID(rel)
		if err != nil {
			// Skip files that don't match the expected layout.
			return nil
		}
		docs.Entries = append(docs.Entries, EntryDocument{ID: id})
		paths = append(paths, path)
		return nil
	})
	if walkErr != nil {
		return GraphDocuments{}, fmt.Errorf("walking graph dir: %w", walkErr)
	}
	if proven {
		cached, hit, err := s.cache.Load(s.dir, stamps)
		if err != nil {
			return GraphDocuments{}, fmt.Errorf("loading the graph cache: %w", err)
		}
		if hit {
			return cached, nil
		}
	}
	errs := make([]error, len(paths))
	forEachConcurrently(len(paths), func(i int) {
		data, err := os.ReadFile(paths[i])
		if err != nil {
			errs[i] = fmt.Errorf("reading %s: %w", paths[i], err)
			return
		}
		docs.Entries[i].Raw = data
		docs.Entries[i].Attachments = s.scanAttachments(docs.Entries[i].ID, dirs)
	})
	if err := errors.Join(errs...); err != nil {
		return GraphDocuments{}, err
	}
	// The stamps were taken before the reads, so a file written in between is
	// saved under its earlier stamp and misses on the next load.
	if proven {
		if err := s.cache.Save(s.dir, stamps, docs); err != nil {
			return GraphDocuments{}, fmt.Errorf("saving the graph cache: %w", err)
		}
	}
	return docs, nil
}

// scanAttachments lists the files in an entry's co-located attachment
// directory, returning graph-relative paths. dirs holds every directory the
// walk saw, so an entry without one (the common case) costs no lookup.
func (s filesystemSource) scanAttachments(id string, dirs map[string]bool) []string {
	attachRel, err := model.AttachDirRelPath(id)
	if err != nil || !dirs[filepath.Clean(attachRel)] {
		return nil
	}
	files, err := os.ReadDir(filepath.Join(s.dir, attachRel))
	if err != nil {
		return nil // no attachment directory
	}
	var attachments []string
	for _, file := range files {
		if !file.IsDir() {
			attachments = append(attachments, filepath.Join(attachRel, file.Name()))
		}
	}
	return attachments
}

// LoadGraph reads all .md files from dir (hierarchical YYYY/MM/ layout),
// joins the base procedure entries embedded in the binary, and builds the
// graph. Base entries are always loaded — a project graph never contains
// them on disk, but its entries may supersede them (procedure
// customization). On the unlikely ID collision, the disk entry wins: a
// project owns its graph directory.
//
// This is now a thin adapter: the filesystem DocumentSource supplies the raw
// documents and GraphFinder applies the single semantic gate (parse, base
// merge, partial-read LoadIssues). The signature is kept so CLI and handler
// callers are untouched; the graph's directory is recorded for provenance and
// lazy WIP resolution.
func (f *Finder) LoadGraph(dir string) (*model.Graph, error) {
	docs, err := filesystemSource{dir: dir, cache: f.graphCache}.GraphDocuments()
	if err != nil {
		return nil, err
	}
	// The CLI path needs only the graph; WIP markers are resolved lazily by
	// GraphFinder.WIPMarkers from the recorded directory when a layout asks.
	g, _, err := buildGraph(docs)
	if err != nil {
		return nil, err
	}
	g.SetGraphDir(dir)
	return g, nil
}
