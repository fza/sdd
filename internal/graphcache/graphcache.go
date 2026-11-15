// Package graphcache keeps a graph directory's raw entry documents in one file
// per graph under a cache root, so a load of an unchanged graph reads that file
// instead of thousands of entry files.
//
// The cache is checked, never trusted: a file whose magic, format version or
// checksum does not match, or that was saved for another directory or other
// file stamps, is a miss, and the caller reads the graph from its files. A
// write lands by rename, so a reader sees a whole file or none.
package graphcache

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/networkteam/sdd/internal/filestamp"
	"github.com/networkteam/sdd/internal/finders"
)

const (
	headerSize = 8 + 1 + sha256.Size
	format     = 1
	tempPrefix = ".graph-"
	// strayAge is how old a temporary file must be before it counts as left
	// by a killed writer; a live writer finishes in far less.
	strayAge = time.Hour
)

var magic = [8]byte{'S', 'D', 'D', 'G', 'R', 'A', 'P', 'H'}

// Cache is a finders.GraphCache over the files under one root directory.
type Cache struct {
	root string
}

var _ finders.GraphCache = (*Cache)(nil)

// New returns a cache keeping its files under root.
func New(root string) *Cache {
	return &Cache{root: root}
}

type payload struct {
	Dir     string
	Stamps  map[string]filestamp.Stamp
	Entries []entry
}

type entry struct {
	ID          string
	Raw         []byte
	Attachments []string
}

func (c *Cache) path(dir string) (string, string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(abs))
	return abs, filepath.Join(c.root, hex.EncodeToString(sum[:])[:16]+".graph"), nil
}

// Load returns the documents saved for dir under exactly stamps.
func (c *Cache) Load(dir string, stamps map[string]filestamp.Stamp) (finders.GraphDocuments, bool, error) {
	abs, path, err := c.path(dir)
	if err != nil {
		return finders.GraphDocuments{}, false, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return finders.GraphDocuments{}, false, nil
	}
	if err != nil {
		return finders.GraphDocuments{}, false, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(data) < headerSize || [8]byte(data[:8]) != magic || data[8] != format {
		return finders.GraphDocuments{}, false, nil
	}
	body := data[headerSize:]
	if sha256.Sum256(body) != [sha256.Size]byte(data[9:headerSize]) {
		return finders.GraphDocuments{}, false, nil
	}
	var saved payload
	if err := gob.NewDecoder(bytes.NewReader(body)).Decode(&saved); err != nil {
		return finders.GraphDocuments{}, false, nil
	}
	if saved.Dir != abs || !maps.Equal(saved.Stamps, stamps) {
		return finders.GraphDocuments{}, false, nil
	}
	docs := finders.GraphDocuments{Entries: make([]finders.EntryDocument, len(saved.Entries))}
	for i, saved := range saved.Entries {
		docs.Entries[i] = finders.EntryDocument{ID: saved.ID, Raw: saved.Raw, Attachments: saved.Attachments}
	}
	return docs, true, nil
}

// Save replaces the documents kept for dir. Only raw documents are kept; a
// structured document or a load issue is refused, since this cache never
// reconstructs either.
func (c *Cache) Save(dir string, stamps map[string]filestamp.Stamp, docs finders.GraphDocuments) error {
	abs, path, err := c.path(dir)
	if err != nil {
		return err
	}
	if len(docs.WIP) > 0 || len(docs.Unreadable) > 0 {
		return errors.New("graph cache keeps raw entry documents only")
	}
	saved := payload{Dir: abs, Stamps: stamps, Entries: make([]entry, len(docs.Entries))}
	for i, doc := range docs.Entries {
		if doc.Raw == nil {
			return errors.New("graph cache keeps raw entry documents only")
		}
		saved.Entries[i] = entry{ID: doc.ID, Raw: doc.Raw, Attachments: doc.Attachments}
	}
	var body bytes.Buffer
	if err := gob.NewEncoder(&body).Encode(saved); err != nil {
		return err
	}
	sum := sha256.Sum256(body.Bytes())
	if err := os.MkdirAll(c.root, 0o755); err != nil {
		return err
	}
	if err := c.removeStrayTemps(); err != nil {
		return err
	}
	temp, err := os.CreateTemp(c.root, tempPrefix)
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	header := append(append(magic[:], format), sum[:]...)
	_, writeErr := temp.Write(append(header, body.Bytes()...))
	if err := errors.Join(writeErr, temp.Close()); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func (c *Cache) removeStrayTemps() error {
	entries, err := os.ReadDir(c.root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), tempPrefix) {
			continue
		}
		info, err := entry.Info()
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if time.Since(info.ModTime()) < strayAge {
			continue
		}
		if err := os.Remove(filepath.Join(c.root, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
