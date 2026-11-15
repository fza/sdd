package cliapp

import (
	"context"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

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
}

// fileStamp is what serve compares to tell a config file changed, appeared
// or vanished.
type fileStamp struct {
	path    string
	exists  bool
	size    int64
	modTime time.Time
}

func stampFiles(paths []string) []fileStamp {
	stamps := make([]fileStamp, 0, len(paths))
	for _, path := range paths {
		stamp := fileStamp{path: path}
		info, err := os.Stat(path)
		if err == nil {
			stamp.exists, stamp.size, stamp.modTime = true, info.Size(), info.ModTime()
		}
		stamps = append(stamps, stamp)
	}
	return stamps
}

// configWatch keeps serve on its current config. Before every tool call it
// stats the config files, a stat being cheap enough for the call path, and a
// change, appearance or removal rebuilds the served project before the call
// is answered. A build that fails refuses every call until one succeeds:
// serve never keeps the old config silently.
type configWatch struct {
	watched []string
	build   func() (*servedProject, error)

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

func newConfigWatch(watched []string, build func() (*servedProject, error)) (*configWatch, error) {
	stamps := stampFiles(watched)
	project, err := build()
	if err != nil {
		return nil, err
	}
	return &configWatch{watched: watched, build: build, current: project, stamps: stamps, told: map[string]int{}}, nil
}

// admit follows the config for one tool call and hands over the notices the
// session the call names, "" for none, was not told yet.
func (w *configWatch) admit(_ context.Context, session string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	err := w.follow()
	if err != nil {
		return "", err
	}
	if session == "" {
		notice := strings.Join(w.notices[w.toldNoneOf:], "\n")
		w.toldNoneOf = len(w.notices)
		return notice, nil
	}
	notice := strings.Join(w.notices[w.told[session]:], "\n")
	w.told[session] = len(w.notices)
	return notice, nil
}

func (w *configWatch) follow() error {
	stamps := stampFiles(w.watched)
	// A broken build retries on every call: a fix written within the same
	// mtime tick and size leaves the stamps as they were.
	if w.broken == nil && slices.Equal(stamps, w.stamps) {
		return nil
	}
	w.stamps = stamps
	next, err := w.build()
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

// opened records a session this serve opened as told every notice so far.
func (w *configWatch) opened(id sdd.SessionID) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.told[string(id)] = len(w.notices)
}

func (w *configWatch) served() *servedProject {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.current
}

// servedAccess resolves every call against the project the config watch
// serves now.
type servedAccess struct{ watch *configWatch }

func (a servedAccess) access() *localRuntimeAccess { return a.watch.served().access }

func (a servedAccess) ResolvePrincipal(ctx context.Context, identity sdd.RequestIdentity) (sdd.Principal, error) {
	return a.access().ResolvePrincipal(ctx, identity)
}

func (a servedAccess) ResolveParticipant(ctx context.Context, principal sdd.Principal, project sdd.ProjectID) (string, error) {
	return a.access().ResolveParticipant(ctx, principal, project)
}

func (a servedAccess) ListProjects(ctx context.Context, principal sdd.Principal) (sdd.ProjectList, error) {
	return a.access().ListProjects(ctx, principal)
}

func (a servedAccess) ResolveProject(ctx context.Context, principal sdd.Principal, project sdd.ProjectID, access sdd.Access) (*sdd.ProjectRuntime, error) {
	return a.access().ResolveProject(ctx, principal, project, access)
}

func (a servedAccess) AuthorizeSession(ctx context.Context, request sdd.SessionAccessRequest) error {
	return a.access().AuthorizeSession(ctx, request)
}

func (a servedAccess) ResolveDependency(ctx context.Context, principal sdd.Principal, project sdd.ProjectID, dependency sdd.RepoID) (*sdd.ProjectRuntime, error) {
	return a.access().ResolveDependency(ctx, principal, project, dependency)
}

// servedSessions reads and writes sessions where the served project keeps
// them now.
type servedSessions struct{ watch *configWatch }

func (s servedSessions) store() sdd.SessionStore { return s.watch.served().sessions }

func (s servedSessions) Create(ctx context.Context, metadata sdd.SessionMetadata) (sdd.StoredSession, error) {
	stored, err := s.store().Create(ctx, metadata)
	if err != nil {
		return stored, err
	}
	s.watch.opened(stored.Metadata.ID)
	return stored, nil
}

func (s servedSessions) Load(ctx context.Context, id sdd.SessionID) (sdd.StoredSession, error) {
	return s.store().Load(ctx, id)
}

func (s servedSessions) List(ctx context.Context, filter sdd.SessionFilter) (sdd.SessionPage, error) {
	return s.store().List(ctx, filter)
}

func (s servedSessions) Append(ctx context.Context, id sdd.SessionID, version uint64, appended sdd.SessionAppend) (uint64, error) {
	return s.store().Append(ctx, id, version, appended)
}

func (s servedSessions) Delete(ctx context.Context, id sdd.SessionID) error {
	return s.store().Delete(ctx, id)
}

// servedBlobs stages attachments where the served project keeps them now.
type servedBlobs struct{ watch *configWatch }

func (b servedBlobs) store() sdd.StagedBlobStore { return b.watch.served().blobs }

func (b servedBlobs) Stage(ctx context.Context, ref sdd.SessionRef, filename string, content io.Reader) (sdd.StagedBlob, error) {
	return b.store().Stage(ctx, ref, filename, content)
}

func (b servedBlobs) Open(ctx context.Context, ref sdd.SessionRef, id string) (io.ReadCloser, error) {
	return b.store().Open(ctx, ref, id)
}

func (b servedBlobs) DeleteStaged(ctx context.Context, ref sdd.SessionRef) error {
	return b.store().DeleteStaged(ctx, ref)
}
