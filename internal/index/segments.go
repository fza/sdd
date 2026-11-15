// Search segments: the read path's compact copy of the published rows.
//
// chromem persists one gob file per row, and opening it decodes every file:
// a search on a 15k-row store opened 15k files, 1 s with the OS file cache
// warm and 16 s with it cold. Segments hold the same rows as a few large
// immutable files under <store>/segments/, so a search costs a handful of
// mapped files and an exact scan, whatever the cache state:
//
//	header   magic, format, dims, row count, table length
//	table    per row: id, payload length (uvarints)
//	padding  zeros up to a 64-byte boundary
//	vectors  count*dims little-endian float32, normalized as chromem stores them
//	payloads per row: JSON {content, metadata}, read only for the hits
//
// A segment is mapped read-only and shared, and its vector block is read in
// place, so every process searching a store shares one copy in the page
// cache: a running sdd serve keeps the pages a CLI search then finds warm.
// A segment whose header names another format is skipped by searches, which
// read its rows from their gob files, and removed by the next refresh.
//
// The chromem rows and the manifest stay the authority; segments are derived.
// A write session refreshes them under the exclusive lock: one new segment
// holding only the rows the manifest names and no segment has, merged into
// one when that would exceed maxSegments or when rows the manifest no longer
// names outnumber the live ones. The same refresh removes temporary segment
// files a killed writer left behind and segments in another format. A reader never writes: rows it finds
// missing, written by a binary that knows no segments, it decodes from their
// gob files. The scan is exact, so results equal a chromem query over the
// same rows.
//
// A segment file is only ever created by rename and removed by unlink, and a
// mapping is released only after the last search holding it closes, so sdd
// never reads an unmapped or shrinking file; guardFaults covers what another
// program does to one.
//
// Rejected: HNSW (approximate recall that post-hoc filters shrink further, a
// graph to build and repair, no measurable gain below ~100k rows), sqlite-vec
// (CGO or a WASM runtime, every writer migrated, a pre-1.0 extension, and its
// KNN is a full scan too), decoding the gob files in parallel (still one open
// per row, so a cold cache stays slow), a resident search daemon (a process to
// supervise and version; the shared page cache already carries warmth across
// processes).
package index

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	chromem "github.com/philippgille/chromem-go"

	"github.com/networkteam/sdd/internal/timing"
)

const (
	segmentsSubdir    = "segments"
	segmentExt        = ".seg"
	segmentTempPrefix = ".segment-"
	segmentHeaderSize = 24
	segmentFormat     = 2
	vectorAlignment   = 64
	maxSegments       = 8
	refreshLockBound  = time.Second
)

var segmentMagic = [7]byte{'S', 'D', 'D', 'S', 'E', 'G', 0}

var nativeLittleEndian = binary.NativeEndian.Uint16([]byte{1, 0}) == 1

// ErrSegmentCorrupt reports a segment file that does not parse.
var ErrSegmentCorrupt = errors.New("index segment is corrupt")

// errForeignFormat marks a well-formed segment header naming a format this
// binary does not write.
var errForeignFormat = errors.New("index segment has another format")

type segmentPayload struct {
	Content  string            `json:"content"`
	Metadata map[string]string `json:"metadata"`
}

type segmentRow struct {
	id      string
	vector  []float32
	payload []byte
}

// segmentFile is one mapped segment. It holds a reference count: the mapping
// is released when the last holder (a store view, a snapshot) lets go.
type segmentFile struct {
	path          string
	mapping       []byte
	unmap         func() error
	refs          atomic.Int64
	dims          int
	ids           []string
	payloadOffset []int
	payloadLength []int
	vectors       []float32
}

func segmentsDir(indexDir string) string {
	return filepath.Join(indexDir, segmentsSubdir)
}

func openSegment(path string) (*segmentFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	mapping, unmap, err := mapSegment(file, int(info.Size()))
	if err := errors.Join(err, file.Close()); err != nil {
		if unmap != nil {
			_ = unmap()
		}
		return nil, fmt.Errorf("mapping segment %s: %w", path, err)
	}
	segment := &segmentFile{path: path, mapping: mapping, unmap: unmap}
	segment.refs.Store(1)
	if err := guardFaults(segment.parse); err != nil {
		_ = unmap()
		return nil, fmt.Errorf("reading segment %s: %w", path, err)
	}
	return segment, nil
}

func (s *segmentFile) parse() error {
	data := s.mapping
	if len(data) < segmentHeaderSize || [7]byte(data[:7]) != segmentMagic {
		return fmt.Errorf("%w: unknown format", ErrSegmentCorrupt)
	}
	if data[7] != segmentFormat {
		return fmt.Errorf("%w: %d", errForeignFormat, data[7])
	}
	dims := uint64(binary.LittleEndian.Uint32(data[8:12]))
	count := uint64(binary.LittleEndian.Uint32(data[12:16]))
	tableLength := binary.LittleEndian.Uint64(data[16:24])
	size := uint64(len(data))
	if tableLength > size-segmentHeaderSize {
		return fmt.Errorf("%w: truncated", ErrSegmentCorrupt)
	}
	vectorsOffset := alignUp(segmentHeaderSize+tableLength, vectorAlignment)
	vectorBytes := count * dims * 4
	if count != 0 && vectorBytes/count != dims*4 || vectorsOffset > size || vectorBytes > size-vectorsOffset {
		return fmt.Errorf("%w: truncated", ErrSegmentCorrupt)
	}
	table := data[segmentHeaderSize : segmentHeaderSize+tableLength]
	s.ids = make([]string, 0, count)
	s.payloadOffset = make([]int, 0, count)
	s.payloadLength = make([]int, 0, count)
	offset := vectorsOffset + vectorBytes
	for range count {
		idLength, n := binary.Uvarint(table)
		if n <= 0 || idLength > uint64(len(table)-n) {
			return fmt.Errorf("%w: table entry", ErrSegmentCorrupt)
		}
		table = table[n:]
		id := string(table[:idLength])
		table = table[idLength:]
		payloadLength, n := binary.Uvarint(table)
		if n <= 0 || payloadLength > size-offset {
			return fmt.Errorf("%w: table entry", ErrSegmentCorrupt)
		}
		table = table[n:]
		s.ids = append(s.ids, id)
		s.payloadOffset = append(s.payloadOffset, int(offset))
		s.payloadLength = append(s.payloadLength, int(payloadLength))
		offset += payloadLength
	}
	s.dims = int(dims)
	adviseWillNeed(data, int(vectorsOffset), int(vectorsOffset+vectorBytes))
	s.vectors = s.vectorBlock(int(vectorsOffset), int(count*dims))
	return nil
}

// vectorBlock views the vector block in place; a platform that is not
// little-endian, or a block a heap read left unaligned, gets a decoded copy.
func (s *segmentFile) vectorBlock(offset, n int) []float32 {
	if n == 0 {
		return nil
	}
	raw := s.mapping[offset : offset+n*4]
	if nativeLittleEndian && uintptr(unsafe.Pointer(&raw[0]))%4 == 0 {
		return unsafe.Slice((*float32)(unsafe.Pointer(&raw[0])), n)
	}
	vectors := make([]float32, n)
	for i := range vectors {
		vectors[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return vectors
}

func alignUp(offset, alignment uint64) uint64 {
	return (offset + alignment - 1) / alignment * alignment
}

func (s *segmentFile) vector(row int) []float32 {
	return s.vectors[row*s.dims : (row+1)*s.dims : (row+1)*s.dims]
}

// payload copies a row's payload out of the mapping, so no byte of it outlives
// the segment's references.
func (s *segmentFile) payload(row int) ([]byte, error) {
	var data []byte
	err := guardFaults(func() error {
		offset := s.payloadOffset[row]
		data = bytes.Clone(s.mapping[offset : offset+s.payloadLength[row]])
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("payload of %s: %w", s.ids[row], err)
	}
	return data, nil
}

func (s *segmentFile) retain() {
	s.refs.Add(1)
}

func (s *segmentFile) release() error {
	if s.refs.Add(-1) != 0 {
		return nil
	}
	if err := s.unmap(); err != nil {
		return fmt.Errorf("unmapping segment %s: %w", s.path, err)
	}
	return nil
}

func isSegmentName(name string) bool {
	return !strings.HasPrefix(name, segmentTempPrefix) && strings.HasSuffix(name, segmentExt)
}

// segmentNames lists the store's segment files, sorted. Names are content
// hashes, so an unchanged list means unchanged segments.
func segmentNames(indexDir string) ([]string, error) {
	entries, err := os.ReadDir(segmentsDir(indexDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing index segments: %w", err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && isSegmentName(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

func strayTempSegments(indexDir string) ([]string, error) {
	entries, err := os.ReadDir(segmentsDir(indexDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing index segments: %w", err)
	}
	var strays []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), segmentTempPrefix) {
			strays = append(strays, filepath.Join(segmentsDir(indexDir), entry.Name()))
		}
	}
	return strays, nil
}

// writeSegment writes rows to a temporary file and renames it to the hash of
// its content, so a reader only ever lists complete segments.
func writeSegment(indexDir string, dims int, rows []segmentRow) (string, error) {
	dir := segmentsDir(indexDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating index segments dir: %w", err)
	}
	temp, err := os.CreateTemp(dir, segmentTempPrefix)
	if err != nil {
		return "", err
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()

	var table []byte
	for _, row := range rows {
		table = binary.AppendUvarint(table, uint64(len(row.id)))
		table = append(table, row.id...)
		table = binary.AppendUvarint(table, uint64(len(row.payload)))
	}
	digest := sha256.New()
	writer := bufio.NewWriterSize(io.MultiWriter(temp, digest), 1<<20)
	if err := encodeSegment(writer, dims, table, rows); err != nil {
		_ = temp.Close()
		return "", err
	}
	if err := errors.Join(writer.Flush(), temp.Sync(), temp.Close()); err != nil {
		return "", err
	}
	final := filepath.Join(dir, hex.EncodeToString(digest.Sum(nil))[:16]+segmentExt)
	if err := os.Rename(tempPath, final); err != nil {
		return "", err
	}
	return final, syncPublicationDirectory(dir)
}

func encodeSegment(writer *bufio.Writer, dims int, table []byte, rows []segmentRow) error {
	header := make([]byte, segmentHeaderSize)
	copy(header, segmentMagic[:])
	header[7] = segmentFormat
	binary.LittleEndian.PutUint32(header[8:12], uint32(dims))
	binary.LittleEndian.PutUint32(header[12:16], uint32(len(rows)))
	binary.LittleEndian.PutUint64(header[16:24], uint64(len(table)))
	if _, err := writer.Write(header); err != nil {
		return err
	}
	if _, err := writer.Write(table); err != nil {
		return err
	}
	written := uint64(segmentHeaderSize + len(table))
	if _, err := writer.Write(make([]byte, alignUp(written, vectorAlignment)-written)); err != nil {
		return err
	}
	word := make([]byte, 4)
	for _, row := range rows {
		for _, value := range row.vector {
			binary.LittleEndian.PutUint32(word, math.Float32bits(value))
			if _, err := writer.Write(word); err != nil {
				return err
			}
		}
	}
	for _, row := range rows {
		if _, err := writer.Write(row.payload); err != nil {
			return err
		}
	}
	return nil
}

// publishedChunks maps every row the manifest names to the version hash it is
// published under — the read view publishedCollection builds from chromem.
func publishedChunks(manifest *Manifest) map[string]string {
	published := map[string]string{}
	for _, state := range manifest.Entries {
		for _, version := range state.Versions {
			for _, id := range version.ChunkIDs {
				published[id] = version.Hash
			}
		}
	}
	return published
}

// readRowFile decodes one chromem row file into a segment row.
func readRowFile(indexDir, id string) (segmentRow, error) {
	file, err := os.Open(documentPath(indexDir, id))
	if err != nil {
		return segmentRow{}, fmt.Errorf("reading index row %s: %w", id, err)
	}
	defer func() { _ = file.Close() }()
	var document chromem.Document
	if err := gob.NewDecoder(bufio.NewReader(file)).Decode(&document); err != nil {
		return segmentRow{}, fmt.Errorf("decoding index row %s: %w", id, err)
	}
	if document.ID != id {
		return segmentRow{}, fmt.Errorf("index row file for %s holds %s", id, document.ID)
	}
	payload, err := json.Marshal(segmentPayload{Content: document.Content, Metadata: document.Metadata})
	if err != nil {
		return segmentRow{}, err
	}
	return segmentRow{id: id, vector: normalizedVector(document.Embedding), payload: payload}, nil
}

// normalizedVector mirrors chromem's normalization, including its tolerance,
// so scores match a chromem query bit for bit.
func normalizedVector(vector []float32) []float32 {
	var squares float64
	for _, value := range vector {
		squares += float64(value) * float64(value)
	}
	if math.Abs(math.Sqrt(squares)-1) < 1e-6 {
		return vector
	}
	var norm float32
	for _, value := range vector {
		norm += value * value
	}
	norm = float32(math.Sqrt(float64(norm)))
	out := make([]float32, len(vector))
	for i, value := range vector {
		out[i] = value / norm
	}
	return out
}

// RefreshSegments brings the store's search segments up to date with its
// manifest. The check runs under the shared lock, so concurrent searches
// finding nothing to do never wait on each other; a refresh takes the
// exclusive lock and checks again. Each lock is waited for at most
// refreshLockBound, then ErrLockBusy: segments are derived, a reader decodes
// what they lack, and a search must not wait out a long writer here. An empty
// indexDir does nothing.
func RefreshSegments(ctx context.Context, indexDir string) error {
	if indexDir == "" {
		return nil
	}
	defer timing.Start(ctx, "segments-refresh")()
	if err := ensureStoreDir(indexDir); err != nil {
		return err
	}
	view := viewFor(indexDir)
	lock := lockFile(indexDir)
	if err := lockWithin(ctx, lock.TryRLockContext); err != nil {
		return err
	}
	needed, err := view.refreshNeeded()
	if err := errors.Join(err, lock.Unlock()); err != nil {
		return err
	}
	if !needed {
		return nil
	}
	if err := lockWithin(ctx, lock.TryLockContext); err != nil {
		return err
	}
	defer func() { _ = lock.Unlock() }()
	return view.refresh()
}

func lockWithin(ctx context.Context, tryLock func(context.Context, time.Duration) (bool, error)) error {
	waitCtx, cancel := context.WithTimeout(ctx, refreshLockBound)
	defer cancel()
	if _, err := tryLock(waitCtx, boundedRetryInterval); err != nil {
		if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			return ErrLockBusy
		}
		return fmt.Errorf("acquiring index lock: %w", err)
	}
	return nil
}

type refreshPlan struct {
	segments  []*segmentFile
	published map[string]string
	missing   []string
	live      int
	dead      int
	// strays are temporary segment files a writer killed before its rename
	// left behind, and segments in another format. Segments are written only
	// under the exclusive lock, so under it every temporary file is a stray.
	strays []string
}

func planRefresh(indexDir string, segments []*segmentFile, foreign []string, published map[string]string) (*refreshPlan, error) {
	strays, err := strayTempSegments(indexDir)
	if err != nil {
		return nil, err
	}
	plan := &refreshPlan{segments: segments, published: published, strays: append(strays, foreign...)}
	seen := map[string]bool{}
	for _, segment := range segments {
		for _, id := range segment.ids {
			if _, ok := plan.published[id]; ok && !seen[id] {
				seen[id] = true
				plan.live++
			} else {
				plan.dead++
			}
		}
	}
	for id := range plan.published {
		if !seen[id] {
			plan.missing = append(plan.missing, id)
		}
	}
	sort.Strings(plan.missing)
	return plan, nil
}

func (p *refreshPlan) compact() bool {
	return (len(p.missing) > 0 && len(p.segments) >= maxSegments) || p.dead > p.live
}

func (p *refreshPlan) noop() bool {
	return len(p.missing) == 0 && len(p.strays) == 0 && !p.compact()
}

// apply writes the planned segment. Compaction copies vectors out of mapped
// segments, so the caller runs it under guardFaults.
func (p *refreshPlan) apply(indexDir string) error {
	for _, stray := range p.strays {
		if err := os.Remove(stray); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing stray index segment: %w", err)
		}
	}
	rows := make([]segmentRow, 0, len(p.missing))
	if p.compact() {
		kept := map[string]bool{}
		for _, segment := range p.segments {
			for row, id := range segment.ids {
				if _, ok := p.published[id]; !ok || kept[id] {
					continue
				}
				payload, err := segment.payload(row)
				if err != nil {
					return err
				}
				kept[id] = true
				rows = append(rows, segmentRow{id: id, vector: segment.vector(row), payload: payload})
			}
		}
	}
	for _, id := range p.missing {
		row, err := readRowFile(indexDir, id)
		if err != nil {
			return err
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 && !p.compact() {
		return nil
	}
	dims := 0
	for _, row := range rows {
		if dims == 0 {
			dims = len(row.vector)
		}
		if len(row.vector) != dims {
			return errors.New("index: inconsistent vector dimensions across rows")
		}
	}
	written := ""
	if len(rows) > 0 {
		path, err := writeSegment(indexDir, dims, rows)
		if err != nil {
			return fmt.Errorf("writing index segment: %w", err)
		}
		written = path
	}
	if !p.compact() {
		return nil
	}
	for _, segment := range p.segments {
		if segment.path == written {
			continue
		}
		if err := os.Remove(segment.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing merged index segment: %w", err)
		}
	}
	return nil
}

// SearchSnapshot is one search's hold on a store's published rows. It holds
// no lock: segment files are immutable, and the mapped segments it reads stay
// mapped until Close, whatever a merge in any process deletes meanwhile.
type SearchSnapshot struct {
	data    *snapshotData
	rebuilt bool
	closed  atomic.Bool
}

type snapshotData struct {
	view     *storeView
	indexDir string
	dims     int
	rows     []snapshotRow
	segments []*segmentFile
	refs     atomic.Int64
}

type snapshotRow struct {
	id      string
	version string
	vector  []float32
	segment *segmentFile
	index   int
	payload []byte
}

func (d *snapshotData) release() error {
	if d.refs.Add(-1) != 0 {
		return nil
	}
	var err error
	for _, segment := range d.segments {
		err = errors.Join(err, segment.release())
	}
	return err
}

// OpenSearchSnapshot loads the store's published rows for a search, waiting
// at most bound for the shared lock (ErrLockBusy past it; a bound of zero waits
// until ctx ends). Rows the segments lack are decoded from their chromem files.
// The rows are reused across calls in this process while the store's manifest
// and segment files are unchanged.
func OpenSearchSnapshot(ctx context.Context, indexDir string, bound time.Duration) (*SearchSnapshot, error) {
	if indexDir == "" {
		return nil, errors.New("index dir is required")
	}
	if err := ensureStoreDir(indexDir); err != nil {
		return nil, err
	}
	waitCtx, cancel := ctx, context.CancelFunc(func() {})
	if bound > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, bound)
	}
	defer cancel()
	lock := lockFile(indexDir)
	stopWait := timing.Start(ctx, "index-lock-wait")
	_, err := lock.TryRLockContext(waitCtx, boundedRetryInterval)
	stopWait()
	if err != nil {
		if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			return nil, ErrLockBusy
		}
		return nil, fmt.Errorf("acquiring index read lock at %s: %w", indexDir, err)
	}
	defer func() { _ = lock.Unlock() }()
	return viewFor(indexDir).acquire(ctx)
}

// Path returns the store directory the snapshot was loaded from.
func (s *SearchSnapshot) Path() string { return s.data.indexDir }

// Count returns the number of published rows in the snapshot.
func (s *SearchSnapshot) Count() int { return len(s.data.rows) }

// Rebuilt reports whether opening this snapshot rebuilt the process's row set,
// rather than reusing it for an unchanged store.
func (s *SearchSnapshot) Rebuilt() bool { return s.rebuilt }

// Close releases the snapshot's hold on the mapped segments. Further calls do
// nothing.
func (s *SearchSnapshot) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	return s.data.release()
}

// Query returns the top nResults published rows by cosine similarity, the
// same hits Index.Query returns over the same rows.
func (s *SearchSnapshot) Query(ctx context.Context, embedding []float32, nResults int) ([]Hit, error) {
	if s.closed.Load() {
		return nil, errors.New("index: query on a closed search snapshot")
	}
	hits, err := s.query(ctx, embedding, nResults)
	if errors.Is(err, ErrSegmentCorrupt) {
		s.data.view.invalidate()
	}
	return hits, err
}

func (s *SearchSnapshot) query(ctx context.Context, embedding []float32, nResults int) ([]Hit, error) {
	data := s.data
	if len(embedding) == 0 {
		return nil, errors.New("query embedding is empty")
	}
	nResults = min(nResults, len(data.rows))
	if nResults <= 0 {
		return nil, nil
	}
	if len(embedding) != data.dims {
		return nil, errors.New("vector query: vectors must have the same length")
	}
	stopScan := timing.Start(ctx, "index-scan")
	query := normalizedVector(embedding)
	type scored struct {
		row   int
		score float32
	}
	scores := make([]scored, len(data.rows))
	err := guardFaults(func() error {
		for i, row := range data.rows {
			var dot float32
			for j, value := range row.vector {
				dot += value * query[j]
			}
			scores[i] = scored{row: i, score: dot}
		}
		return nil
	})
	if err != nil {
		stopScan()
		return nil, err
	}
	slices.SortFunc(scores, func(a, b scored) int {
		switch {
		case a.score > b.score:
			return -1
		case a.score < b.score:
			return 1
		}
		return strings.Compare(data.rows[a.row].id, data.rows[b.row].id)
	})
	stopScan()
	defer timing.Start(ctx, "index-hits-read")()
	hits := make([]Hit, 0, nResults)
	for _, top := range scores[:nResults] {
		row := data.rows[top.row]
		payloadData := row.payload
		if payloadData == nil {
			var err error
			payloadData, err = row.segment.payload(row.index)
			if err != nil {
				return nil, err
			}
		}
		var payload segmentPayload
		if err := json.Unmarshal(payloadData, &payload); err != nil {
			return nil, fmt.Errorf("%w: payload of %s: %w", ErrSegmentCorrupt, row.id, err)
		}
		if payload.Metadata == nil {
			payload.Metadata = map[string]string{}
		}
		if hash := payload.Metadata[MetaEntryHash]; hash != "" && hash != row.version {
			return nil, fmt.Errorf("index: published chunk version mismatch")
		}
		payload.Metadata[MetaEntryHash] = row.version
		hits = append(hits, hitFromResult(chromem.Result{ID: row.id, Metadata: payload.Metadata, Content: payload.Content, Similarity: top.score}))
	}
	return hits, nil
}
