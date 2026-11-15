package cliapp

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// servedProject is what serve answers calls with under one resolution of its
// config files.
type servedProject struct {
	access   *localRuntimeAccess
	cfg      *model.PerRepoConfig
	sessions sdd.SessionStore
	blobs    sdd.StagedBlobStore
	layers   model.ConfigLayers
	clients  *servedClients
}

// fileStamp is what serve compares to tell a config file changed, appeared
// or vanished. It hashes the content: a retargeted symlink, a copy keeping
// mtimes, or a same-size write within one mtime tick leaves size and mtime
// as they were.
type fileStamp struct {
	path     string
	readable bool
	sum      [sha256.Size]byte
}

// configRead is one read of a watched file. A rebuild derives everything
// from the reads its stamps were taken from, so an edit landing during the
// rebuild cannot mix two versions into one project.
type configRead struct {
	path  string
	data  []byte
	found bool
	// err is a failure other than absence; the build fails loud on it.
	err error
}

func readConfigFiles(paths []string) []configRead {
	reads := make([]configRead, 0, len(paths))
	for _, path := range paths {
		read := configRead{path: path}
		data, err := os.ReadFile(path)
		switch {
		case err == nil:
			read.data, read.found = data, true
		case !errors.Is(err, fs.ErrNotExist):
			read.err = err
		}
		reads = append(reads, read)
	}
	return reads
}

func stampFiles(reads []configRead) []fileStamp {
	stamps := make([]fileStamp, 0, len(reads))
	for _, read := range reads {
		stamp := fileStamp{path: read.path, readable: read.found}
		if read.found {
			stamp.sum = sha256.Sum256(read.data)
		}
		stamps = append(stamps, stamp)
	}
	return stamps
}

// toldLimit bounds how many sessions serve remembers telling. Past it, the
// sessions told every notice are forgotten, and a forgotten session is told
// again what it already was, which beats missing a change.
const toldLimit = 1024

type servedProjectKey struct{}

// configWatch keeps serve on its current config. Before every tool call it
// reads the config files, small enough for the call path, and a change,
// appearance or removal rebuilds the served project before the call is
// answered. A build that fails refuses every call until one succeeds: serve
// never keeps the old config silently.
type configWatch struct {
	watched []string
	build   func(previous *servedProject, reads []configRead) (*servedProject, error)

	mu      sync.Mutex
	current *servedProject
	stamps  []fileStamp
	broken  error
	// notices holds one notice per rebuild that changed a key, oldest first.
	// told counts how many of them a session was handed; a session this
	// serve did not open was open across every one.
	notices    []string
	told       map[string]int
	toldNoneOf int
}

func newConfigWatch(watched []string, build func(previous *servedProject, reads []configRead) (*servedProject, error)) (*configWatch, error) {
	reads := readConfigFiles(watched)
	stamps := stampFiles(reads)
	project, err := build(nil, reads)
	if err != nil {
		return nil, err
	}
	return &configWatch{watched: watched, build: build, current: project, stamps: stamps, told: map[string]int{}}, nil
}

// admit follows the config for one tool call, pins the call to the project
// served now, and hands over the notices the session the call names, "" for
// none, was not told yet.
func (w *configWatch) admit(ctx context.Context, session string) (context.Context, string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	err := w.follow()
	if err != nil {
		return ctx, "", err
	}
	call := context.WithValue(ctx, servedProjectKey{}, w.current)
	if session == "" {
		notice := strings.Join(w.notices[w.toldNoneOf:], "\n")
		w.toldNoneOf = len(w.notices)
		return call, notice, nil
	}
	notice := strings.Join(w.notices[w.told[session]:], "\n")
	w.tell(session)
	return call, notice, nil
}

func (w *configWatch) follow() error {
	reads := readConfigFiles(w.watched)
	stamps := stampFiles(reads)
	// A broken build retries on every call, so a transient failure clears
	// without another edit.
	if w.broken == nil && slices.Equal(stamps, w.stamps) {
		return nil
	}
	w.stamps = stamps
	next, err := w.build(w.current, reads)
	if err != nil {
		w.broken = err
		return err
	}
	w.broken = nil
	notice := model.FormatConfigNotice(model.ConfigChanges(w.current.layers, next.layers))
	if notice != "" {
		w.notices = append(w.notices, notice)
	}
	w.current = next
	return nil
}

// tell records session as told every notice so far.
func (w *configWatch) tell(session string) {
	w.told[session] = len(w.notices)
	if len(w.told) <= toldLimit {
		return
	}
	for id, told := range w.told {
		if told == len(w.notices) && id != session {
			delete(w.told, id)
		}
	}
	if len(w.told) > toldLimit {
		clear(w.told)
		w.told[session] = len(w.notices)
	}
}

// opened records a session this serve opened as told every notice so far.
func (w *configWatch) opened(id sdd.SessionID) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.tell(string(id))
}

func (w *configWatch) served() *servedProject {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.current
}

// project is the project a call was pinned to, or the one served now outside
// a call.
func (w *configWatch) project(ctx context.Context) *servedProject {
	pinned, ok := ctx.Value(servedProjectKey{}).(*servedProject)
	if ok {
		return pinned
	}
	return w.served()
}

// servedAccess resolves every call against the project the config watch
// pinned it to.
type servedAccess struct{ watch *configWatch }

func (a servedAccess) access(ctx context.Context) *localRuntimeAccess {
	return a.watch.project(ctx).access
}

func (a servedAccess) ResolvePrincipal(ctx context.Context, identity sdd.RequestIdentity) (sdd.Principal, error) {
	return a.access(ctx).ResolvePrincipal(ctx, identity)
}

func (a servedAccess) ResolveParticipant(ctx context.Context, principal sdd.Principal, project sdd.ProjectID) (string, error) {
	return a.access(ctx).ResolveParticipant(ctx, principal, project)
}

func (a servedAccess) ListProjects(ctx context.Context, principal sdd.Principal) (sdd.ProjectList, error) {
	return a.access(ctx).ListProjects(ctx, principal)
}

func (a servedAccess) ResolveProject(ctx context.Context, principal sdd.Principal, project sdd.ProjectID, access sdd.Access) (*sdd.ProjectRuntime, error) {
	return a.access(ctx).ResolveProject(ctx, principal, project, access)
}

func (a servedAccess) AuthorizeSession(ctx context.Context, request sdd.SessionAccessRequest) error {
	return a.access(ctx).AuthorizeSession(ctx, request)
}

func (a servedAccess) ResolveDependency(ctx context.Context, principal sdd.Principal, project sdd.ProjectID, dependency sdd.RepoID) (*sdd.ProjectRuntime, error) {
	return a.access(ctx).ResolveDependency(ctx, principal, project, dependency)
}

// servedSessions reads and writes sessions where the pinned project keeps
// them.
type servedSessions struct{ watch *configWatch }

func (s servedSessions) store(ctx context.Context) sdd.SessionStore {
	return s.watch.project(ctx).sessions
}

func (s servedSessions) Create(ctx context.Context, metadata sdd.SessionMetadata) (sdd.StoredSession, error) {
	stored, err := s.store(ctx).Create(ctx, metadata)
	if err != nil {
		return stored, err
	}
	s.watch.opened(stored.Metadata.ID)
	return stored, nil
}

func (s servedSessions) Load(ctx context.Context, id sdd.SessionID) (sdd.StoredSession, error) {
	return s.store(ctx).Load(ctx, id)
}

func (s servedSessions) List(ctx context.Context, filter sdd.SessionFilter) (sdd.SessionPage, error) {
	return s.store(ctx).List(ctx, filter)
}

func (s servedSessions) Append(ctx context.Context, id sdd.SessionID, version uint64, appended sdd.SessionAppend) (uint64, error) {
	return s.store(ctx).Append(ctx, id, version, appended)
}

func (s servedSessions) Delete(ctx context.Context, id sdd.SessionID) error {
	return s.store(ctx).Delete(ctx, id)
}

// servedBlobs stages attachments where the pinned project keeps them.
type servedBlobs struct{ watch *configWatch }

func (b servedBlobs) store(ctx context.Context) sdd.StagedBlobStore {
	return b.watch.project(ctx).blobs
}

func (b servedBlobs) Stage(ctx context.Context, ref sdd.SessionRef, filename string, content io.Reader) (sdd.StagedBlob, error) {
	return b.store(ctx).Stage(ctx, ref, filename, content)
}

func (b servedBlobs) Open(ctx context.Context, ref sdd.SessionRef, id string) (io.ReadCloser, error) {
	return b.store(ctx).Open(ctx, ref, id)
}

func (b servedBlobs) DeleteStaged(ctx context.Context, ref sdd.SessionRef) error {
	return b.store(ctx).DeleteStaged(ctx, ref)
}
