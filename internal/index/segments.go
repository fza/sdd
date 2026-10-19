// Search segments: the read path's compact copy of the published rows.
//
// chromem persists one gob file per row, and opening it decodes every file:
// a search on a 15k-row store opened 15k files, 1 s with the OS file cache
// warm and 16 s with it cold. Segments hold the same rows as a few large
// immutable files under <store>/segments/, so a search costs a handful of
// sequential reads and an exact scan, whatever the cache state:
//
//	header   magic, dims, row count, table length
//	table    per row: id, payload length (uvarints)
//	vectors  count*dims little-endian float32, normalized as chromem stores them
//	payloads per row: JSON {content, metadata}, read only for the hits
//
// The chromem rows and the manifest stay the authority; segments are derived.
// A write session refreshes them under the exclusive lock: one new segment
// holding only the rows the manifest names and no segment has, merged into
// one when that would exceed maxSegments or when rows the manifest no longer
// names outnumber the live ones. The same refresh removes temporary segment
// files a killed writer left behind. A reader never writes: rows it finds
// missing, written by a binary that knows no segments, it decodes from their
// gob files.
// The scan is exact, so results equal a chromem query over the same rows.
//
// Rejected: HNSW (approximate recall that post-hoc filters shrink further, a
// graph to build and repair, no measurable gain below ~100k rows), sqlite-vec
// (CGO or a WASM runtime, every writer migrated, a pre-1.0 extension, and its
// KNN is a full scan too), decoding the gob files in parallel (still one open
// per row, so a cold cache stays slow).
package index

import (
	"bufio"
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
	"time"

	chromem "github.com/philippgille/chromem-go"

	"github.com/networkteam/sdd/internal/timing"
)

const (
	segmentsSubdir    = "segments"
	segmentExt        = ".seg"
	segmentTempPrefix = ".segment-"
	segmentHeaderSize = 24
	maxSegments       = 8
	refreshLockBound  = time.Second
)

var segmentMagic = [8]byte{'S', 'D', 'D', 'S', 'E', 'G', 0, 1}

// ErrSegmentCorrupt reports a segment file that does not parse.
var ErrSegmentCorrupt = errors.New("index segment is corrupt")

type segmentPayload struct {
	Content  string            `json:"content"`
	Metadata map[string]string `json:"metadata"`
}

type segmentRow struct {
	id      string
	vector  []float32
	payload []byte
}

type segmentFile struct {
	file          *os.File
	dims          int
	ids           []string
	payloadOffset []int64
	payloadLength []int
	vectorsOffset int64
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
	segment, err := readSegmentTable(file)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("reading segment %s: %w", path, err)
	}
	return segment, nil
}

func readSegmentTable(file *os.File) (*segmentFile, error) {
	header := make([]byte, segmentHeaderSize)
	if _, err := io.ReadFull(file, header); err != nil {
		return nil, fmt.Errorf("%w: header: %w", ErrSegmentCorrupt, err)
	}
	if [8]byte(header[:8]) != segmentMagic {
		return nil, fmt.Errorf("%w: unknown format", ErrSegmentCorrupt)
	}
	dims := int(binary.LittleEndian.Uint32(header[8:12]))
	count := int(binary.LittleEndian.Uint32(header[12:16]))
	tableLength := binary.LittleEndian.Uint64(header[16:24])
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	vectorsOffset := int64(segmentHeaderSize) + int64(tableLength)
	vectorBytes := int64(count) * int64(dims) * 4
	if tableLength > uint64(info.Size()) || vectorsOffset+vectorBytes > info.Size() {
		return nil, fmt.Errorf("%w: truncated", ErrSegmentCorrupt)
	}
	table := make([]byte, tableLength)
	if _, err := io.ReadFull(file, table); err != nil {
		return nil, fmt.Errorf("%w: table: %w", ErrSegmentCorrupt, err)
	}
	segment := &segmentFile{file: file, dims: dims, vectorsOffset: vectorsOffset, ids: make([]string, 0, count), payloadOffset: make([]int64, 0, count), payloadLength: make([]int, 0, count)}
	offset := vectorsOffset + vectorBytes
	for range count {
		idLength, n := binary.Uvarint(table)
		if n <= 0 || idLength > uint64(len(table)-n) {
			return nil, fmt.Errorf("%w: table entry", ErrSegmentCorrupt)
		}
		table = table[n:]
		id := string(table[:idLength])
		table = table[idLength:]
		payloadLength, n := binary.Uvarint(table)
		if n <= 0 {
			return nil, fmt.Errorf("%w: table entry", ErrSegmentCorrupt)
		}
		table = table[n:]
		segment.ids = append(segment.ids, id)
		segment.payloadOffset = append(segment.payloadOffset, offset)
		segment.payloadLength = append(segment.payloadLength, int(payloadLength))
		offset += int64(payloadLength)
	}
	if offset > info.Size() {
		return nil, fmt.Errorf("%w: truncated payloads", ErrSegmentCorrupt)
	}
	return segment, nil
}

func (s *segmentFile) loadVectors() error {
	raw := make([]byte, len(s.ids)*s.dims*4)
	if _, err := s.file.ReadAt(raw, s.vectorsOffset); err != nil {
		return fmt.Errorf("%w: vectors: %w", ErrSegmentCorrupt, err)
	}
	s.vectors = make([]float32, len(s.ids)*s.dims)
	for i := range s.vectors {
		s.vectors[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return nil
}

func (s *segmentFile) vector(row int) []float32 {
	return s.vectors[row*s.dims : (row+1)*s.dims : (row+1)*s.dims]
}

func (s *segmentFile) payload(row int) ([]byte, error) {
	data := make([]byte, s.payloadLength[row])
	if _, err := s.file.ReadAt(data, s.payloadOffset[row]); err != nil {
		return nil, fmt.Errorf("%w: payload of %s: %w", ErrSegmentCorrupt, s.ids[row], err)
	}
	return data, nil
}

func listSegments(indexDir string) ([]*segmentFile, error) {
	entries, err := os.ReadDir(segmentsDir(indexDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing index segments: %w", err)
	}
	var segments []*segmentFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), segmentExt) {
			continue
		}
		segment, err := openSegment(filepath.Join(segmentsDir(indexDir), entry.Name()))
		if err != nil {
			closeSegments(segments)
			return nil, err
		}
		segments = append(segments, segment)
	}
	return segments, nil
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

func closeSegments(segments []*segmentFile) {
	for _, segment := range segments {
		_ = segment.file.Close()
	}
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
	binary.LittleEndian.PutUint32(header[8:12], uint32(dims))
	binary.LittleEndian.PutUint32(header[12:16], uint32(len(rows)))
	binary.LittleEndian.PutUint64(header[16:24], uint64(len(table)))
	if _, err := writer.Write(header); err != nil {
		return err
	}
	if _, err := writer.Write(table); err != nil {
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
	lock := lockFile(indexDir)
	if err := lockWithin(ctx, lock.TryRLockContext); err != nil {
		return err
	}
	plan, err := planRefresh(indexDir)
	if err := errors.Join(err, lock.Unlock()); err != nil {
		return err
	}
	if plan.noop() {
		return nil
	}
	if err := lockWithin(ctx, lock.TryLockContext); err != nil {
		return err
	}
	defer func() { _ = lock.Unlock() }()
	plan, err = planRefresh(indexDir)
	if err != nil {
		return err
	}
	defer closeSegments(plan.segments)
	if plan.noop() {
		return nil
	}
	return plan.apply(indexDir)
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
	// left behind. Segments are written only under the exclusive lock, so
	// under it every temporary file is a stray.
	strays []string
}

func planRefresh(indexDir string) (*refreshPlan, error) {
	manifest, err := LoadManifest(indexDir)
	if err != nil {
		return nil, err
	}
	segments, err := listSegments(indexDir)
	if err != nil {
		return nil, err
	}
	strays, err := strayTempSegments(indexDir)
	if err != nil {
		closeSegments(segments)
		return nil, err
	}
	plan := &refreshPlan{segments: segments, published: publishedChunks(manifest), strays: strays}
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
	if len(p.missing) == 0 && len(p.strays) == 0 && !p.compact() {
		closeSegments(p.segments)
		p.segments = nil
		return true
	}
	return false
}

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
			if err := segment.loadVectors(); err != nil {
				return err
			}
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
		if segment.file.Name() == written {
			continue
		}
		if err := os.Remove(segment.file.Name()); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing merged index segment: %w", err)
		}
	}
	return nil
}

// SearchSnapshot is the published read view of a store, loaded from its
// segments for one search. It holds no lock: segment files are immutable and
// it keeps the ones it read open until Close.
type SearchSnapshot struct {
	indexDir string
	dims     int
	rows     []snapshotRow
	segments []*segmentFile
}

type snapshotRow struct {
	id      string
	version string
	vector  []float32
	segment *segmentFile
	index   int
	payload []byte
}

// OpenSearchSnapshot loads the store's published rows for a search, waiting
// at most bound for the shared lock (ErrLockBusy past it; a bound of zero waits
// until ctx ends). Rows the segments lack are decoded from their chromem files.
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
	snapshot, err := loadSnapshot(ctx, indexDir)
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

func loadSnapshot(ctx context.Context, indexDir string) (*SearchSnapshot, error) {
	stopRead := timing.Start(ctx, "index-segments-read")
	manifest, err := LoadManifest(indexDir)
	if err != nil {
		stopRead()
		return nil, err
	}
	published := publishedChunks(manifest)
	segments, err := listSegments(indexDir)
	if err != nil {
		stopRead()
		return nil, err
	}
	snapshot := &SearchSnapshot{indexDir: indexDir, segments: segments}
	seen := map[string]bool{}
	for _, segment := range segments {
		if err := segment.loadVectors(); err != nil {
			stopRead()
			_ = snapshot.Close()
			return nil, err
		}
		for row, id := range segment.ids {
			version, ok := published[id]
			if !ok || seen[id] {
				continue
			}
			seen[id] = true
			snapshot.rows = append(snapshot.rows, snapshotRow{id: id, version: version, vector: segment.vector(row), segment: segment, index: row})
		}
	}
	stopRead()
	stopDecode := timing.Start(ctx, "index-rows-decode")
	defer stopDecode()
	missing := make([]string, 0)
	for id := range published {
		if !seen[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	for _, id := range missing {
		row, err := readRowFile(indexDir, id)
		if err != nil {
			_ = snapshot.Close()
			return nil, err
		}
		snapshot.rows = append(snapshot.rows, snapshotRow{id: id, version: published[id], vector: row.vector, payload: row.payload})
	}
	for _, row := range snapshot.rows {
		if snapshot.dims == 0 {
			snapshot.dims = len(row.vector)
		}
		if len(row.vector) != snapshot.dims {
			_ = snapshot.Close()
			return nil, errors.New("index: inconsistent vector dimensions across rows")
		}
	}
	return snapshot, nil
}

// Path returns the store directory the snapshot was loaded from.
func (s *SearchSnapshot) Path() string { return s.indexDir }

// Count returns the number of published rows in the snapshot.
func (s *SearchSnapshot) Count() int { return len(s.rows) }

// Close releases the segment files the snapshot reads hit payloads from.
func (s *SearchSnapshot) Close() error {
	var err error
	for _, segment := range s.segments {
		err = errors.Join(err, segment.file.Close())
	}
	s.segments = nil
	return err
}

// Query returns the top nResults published rows by cosine similarity, the
// same hits Index.Query returns over the same rows.
func (s *SearchSnapshot) Query(ctx context.Context, embedding []float32, nResults int) ([]Hit, error) {
	if len(embedding) == 0 {
		return nil, errors.New("query embedding is empty")
	}
	nResults = min(nResults, len(s.rows))
	if nResults <= 0 {
		return nil, nil
	}
	if len(embedding) != s.dims {
		return nil, errors.New("vector query: vectors must have the same length")
	}
	stopScan := timing.Start(ctx, "index-scan")
	query := normalizedVector(embedding)
	type scored struct {
		row   int
		score float32
	}
	scores := make([]scored, len(s.rows))
	for i, row := range s.rows {
		var dot float32
		for j, value := range row.vector {
			dot += value * query[j]
		}
		scores[i] = scored{row: i, score: dot}
	}
	slices.SortFunc(scores, func(a, b scored) int {
		switch {
		case a.score > b.score:
			return -1
		case a.score < b.score:
			return 1
		}
		return strings.Compare(s.rows[a.row].id, s.rows[b.row].id)
	})
	stopScan()
	defer timing.Start(ctx, "index-hits-read")()
	hits := make([]Hit, 0, nResults)
	for _, top := range scores[:nResults] {
		row := s.rows[top.row]
		data := row.payload
		if data == nil {
			var err error
			data, err = row.segment.payload(row.index)
			if err != nil {
				return nil, err
			}
		}
		var payload segmentPayload
		if err := json.Unmarshal(data, &payload); err != nil {
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
