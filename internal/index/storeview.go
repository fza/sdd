package index

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/networkteam/sdd/internal/timing"
)

// idleSweepInterval is how long a process keeps a store view after its last
// search before checking whether the store moved on. A merge deletes segment
// files, and a deleted file keeps its disk space while any process maps it.
const idleSweepInterval = 30 * time.Second

// views holds one storeView per store directory for the life of the process,
// so a command's fill, refresh and search parse the manifest and map the
// segments once, and a long-running server keeps them across requests.
var views sync.Map

// storeView is this process's parsed view of one store: the manifest, the
// mapped segments, and the search rows built from them. It stays valid while
// the manifest file's identity (replaced by rename on every save) and the
// segment file names (content hashes) are unchanged. Syncing it reads the
// segment list, so callers hold the store's shared or exclusive lock while
// they do.
type storeView struct {
	dir string

	mu         sync.Mutex
	manifestID fs.FileInfo
	manifest   *Manifest
	published  map[string]string
	names      []string
	segments   map[string]*segmentFile
	synced     bool
	snapshot   *snapshotData
	rowFiles   map[string]segmentRow
	sweep      *time.Timer
}

func viewFor(indexDir string) *storeView {
	if view, ok := views.Load(indexDir); ok {
		return view.(*storeView)
	}
	view, _ := views.LoadOrStore(indexDir, &storeView{dir: indexDir})
	return view.(*storeView)
}

// cachedManifest returns the store's manifest, parsing it only when the file
// changed since this process last read it. The result is shared: callers must
// not modify it. A missing manifest reads as an empty one.
func cachedManifest(indexDir string) (*Manifest, error) {
	view := viewFor(indexDir)
	view.mu.Lock()
	defer view.mu.Unlock()
	return view.manifestLocked()
}

func (v *storeView) manifestLocked() (_ *Manifest, err error) {
	file, err := os.Open(manifestPath(v.dir))
	if os.IsNotExist(err) {
		if v.manifest == nil || v.manifestID != nil {
			if err := v.setManifestLocked(nil, &Manifest{Version: 1, Entries: map[string]EntryState{}}); err != nil {
				return nil, err
			}
		}
		return v.manifest, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	identity, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if v.manifest != nil && sameFileIdentity(v.manifestID, identity) {
		return v.manifest, nil
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	manifest, err := parseManifest(data)
	if err != nil {
		return nil, err
	}
	if err := v.setManifestLocked(identity, manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

func (v *storeView) setManifestLocked(identity fs.FileInfo, manifest *Manifest) error {
	v.manifestID, v.manifest = identity, manifest
	v.published = publishedChunks(manifest)
	v.rowFiles = nil
	return v.dropSnapshotLocked()
}

func sameFileIdentity(a, b fs.FileInfo) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return os.SameFile(a, b) && a.ModTime().Equal(b.ModTime()) && a.Size() == b.Size()
}

// syncLocked brings the view up to the store on disk: the manifest when its
// file changed, and the mapped segments when the segment names changed —
// mapping only names it has not mapped and releasing names that are gone.
func (v *storeView) syncLocked() error {
	if _, err := v.manifestLocked(); err != nil {
		return errors.Join(err, v.resetLocked())
	}
	names, err := segmentNames(v.dir)
	if err != nil {
		return errors.Join(err, v.resetLocked())
	}
	if v.synced && slices.Equal(names, v.names) {
		return nil
	}
	next := make(map[string]*segmentFile, len(names))
	for _, name := range names {
		if segment, ok := v.segments[name]; ok {
			next[name] = segment
			continue
		}
		segment, err := openSegment(filepath.Join(segmentsDir(v.dir), name))
		if err != nil {
			for opened, kept := range next {
				if v.segments[opened] != kept {
					err = errors.Join(err, kept.release())
				}
			}
			return errors.Join(err, v.resetLocked())
		}
		next[name] = segment
	}
	releaseErr := v.dropSnapshotLocked()
	for name, segment := range v.segments {
		if next[name] != segment {
			releaseErr = errors.Join(releaseErr, segment.release())
		}
	}
	v.segments, v.names, v.synced = next, names, true
	return releaseErr
}

func (v *storeView) orderedSegmentsLocked() []*segmentFile {
	segments := make([]*segmentFile, 0, len(v.names))
	for _, name := range v.names {
		segments = append(segments, v.segments[name])
	}
	return segments
}

func (v *storeView) refreshNeeded() (bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.syncLocked(); err != nil {
		return false, err
	}
	plan, err := planRefresh(v.dir, v.orderedSegmentsLocked(), v.published)
	if err != nil {
		return false, err
	}
	return !plan.noop(), nil
}

// refresh writes the planned segment. The caller holds the exclusive lock; the
// view picks up the changed segment names on its next sync.
func (v *storeView) refresh() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.syncLocked(); err != nil {
		return err
	}
	plan, err := planRefresh(v.dir, v.orderedSegmentsLocked(), v.published)
	if err != nil {
		return err
	}
	if plan.noop() {
		return nil
	}
	return guardFaults(func() error { return plan.apply(v.dir) })
}

// acquire returns a snapshot of the published rows, building the row set only
// when the store changed. The caller holds the shared lock.
func (v *storeView) acquire(ctx context.Context) (*SearchSnapshot, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	stopRead := timing.Start(ctx, "index-segments-read")
	err := v.syncLocked()
	stopRead()
	if err != nil {
		return nil, err
	}
	data, rebuilt, err := v.snapshotLocked(ctx)
	if err != nil {
		return nil, err
	}
	data.refs.Add(1)
	v.armSweepLocked()
	return &SearchSnapshot{data: data, rebuilt: rebuilt}, nil
}

func (v *storeView) snapshotLocked(ctx context.Context) (*snapshotData, bool, error) {
	if v.snapshot != nil {
		return v.snapshot, false, nil
	}
	data := &snapshotData{view: v, indexDir: v.dir}
	seen := map[string]bool{}
	for _, segment := range v.orderedSegmentsLocked() {
		used := false
		for row, id := range segment.ids {
			version, ok := v.published[id]
			if !ok || seen[id] {
				continue
			}
			seen[id], used = true, true
			data.rows = append(data.rows, snapshotRow{id: id, version: version, vector: segment.vector(row), segment: segment, index: row})
		}
		if used {
			segment.retain()
			data.segments = append(data.segments, segment)
		}
	}
	data.refs.Store(1)
	stopDecode := timing.Start(ctx, "index-rows-decode")
	missing := make([]string, 0)
	for id := range v.published {
		if !seen[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	for _, id := range missing {
		row, ok := v.rowFiles[id]
		if !ok {
			var err error
			row, err = readRowFile(v.dir, id)
			if err != nil {
				stopDecode()
				return nil, false, errors.Join(err, data.release())
			}
			if v.rowFiles == nil {
				v.rowFiles = map[string]segmentRow{}
			}
			v.rowFiles[id] = row
		}
		data.rows = append(data.rows, snapshotRow{id: id, version: v.published[id], vector: row.vector, payload: row.payload})
	}
	stopDecode()
	for _, row := range data.rows {
		if data.dims == 0 {
			data.dims = len(row.vector)
		}
		if len(row.vector) != data.dims {
			return nil, false, errors.Join(errors.New("index: inconsistent vector dimensions across rows"), data.release())
		}
	}
	v.snapshot = data
	return data, true, nil
}

func (v *storeView) dropSnapshotLocked() error {
	if v.snapshot == nil {
		return nil
	}
	err := v.snapshot.release()
	v.snapshot = nil
	return err
}

// resetLocked drops everything the view holds. Open searches keep their own
// references and unmap on Close.
func (v *storeView) resetLocked() error {
	err := errors.Join(v.dropSnapshotLocked(), v.releaseSegmentsLocked())
	v.manifestID, v.manifest, v.published, v.rowFiles = nil, nil, nil, nil
	if v.sweep != nil {
		v.sweep.Stop()
		v.sweep = nil
	}
	return err
}

func (v *storeView) releaseSegmentsLocked() error {
	var err error
	for _, segment := range v.segments {
		err = errors.Join(err, segment.release())
	}
	v.segments, v.names, v.synced = nil, nil, false
	return err
}

// invalidate drops the view after a memory fault, so the next search maps the
// segments afresh and reports what is wrong with them.
func (v *storeView) invalidate() {
	v.mu.Lock()
	defer v.mu.Unlock()
	_ = v.resetLocked()
}

func (v *storeView) armSweepLocked() {
	if v.sweep == nil {
		v.sweep = time.AfterFunc(idleSweepInterval, func() {
			// Nothing waits on the timer; a failing check has already
			// dropped the view, and the next search reports the cause.
			_ = ReleaseStale(v.dir)
		})
		return
	}
	v.sweep.Reset(idleSweepInterval)
}

// ReleaseStale drops this process's mapped segments and parsed manifest for
// indexDir when the store changed since they were loaded, so segment files a
// merge in any process deleted stop holding disk space. An unchanged view is
// kept and checked again later; a store locked by a writer is checked later.
// Searches still open keep their segments until they close. Every view checks
// itself this way once idleSweepInterval passes without a search.
func ReleaseStale(indexDir string) error {
	value, ok := views.Load(indexDir)
	if !ok {
		return nil
	}
	view := value.(*storeView)
	view.mu.Lock()
	defer view.mu.Unlock()
	if !view.synced {
		return nil
	}
	lock := lockFile(indexDir)
	locked, err := lock.TryRLock()
	if err != nil {
		return errors.Join(err, view.resetLocked())
	}
	if !locked {
		view.armSweepLocked()
		return nil
	}
	defer func() { _ = lock.Unlock() }()
	identity, err := os.Stat(manifestPath(indexDir))
	if err != nil && !os.IsNotExist(err) {
		return errors.Join(err, view.resetLocked())
	}
	names, err := segmentNames(indexDir)
	if err != nil {
		return errors.Join(err, view.resetLocked())
	}
	if !sameFileIdentity(view.manifestID, identity) || !slices.Equal(names, view.names) {
		return view.resetLocked()
	}
	view.armSweepLocked()
	return nil
}
