package cliapp

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// servedProject is what serve answers calls with under one resolution of its
// config layers.
type servedProject struct {
	access   *localRuntimeAccess
	cfg      *model.PerRepoConfig
	layout   meta.Project
	sessions sdd.SessionStore
	blobs    sdd.StagedBlobStore
	graphDir string
	// entries is how many entries graphDir held when this resolution was
	// built, the count a later move of the graph must reach.
	entries int
	layers  model.ConfigLayers
	// watched are the config files this resolution read or would read once
	// they exist.
	watched []string
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

// configWatch keeps serve on its current config: before a call or a sync tick
// it stats every config file, and a change, appearance or removal rebuilds
// the served project before the call is answered. A build that fails, a
// broken file among them, refuses every call until a build succeeds; serve
// never keeps the old config silently. A config naming another graph folder
// refuses every call until that folder holds the graph and the one served
// before holds none of it, and only then is the project rebuilt on it.
type configWatch struct {
	locate func() (string, error)
	build  func() (*servedProject, error)
	// guards run before every call once the config is followed, and refuse
	// the call while their state lasts.
	guards []func(context.Context, *servedProject) error

	mu      sync.Mutex
	current *servedProject
	watched []string
	stamps  []fileStamp
	broken  error
	moved   *model.GraphRelocation
	notices []string
}

func newConfigWatch(locate func() (string, error), build func() (*servedProject, error)) (*configWatch, error) {
	project, err := build()
	if err != nil {
		return nil, err
	}
	return &configWatch{locate: locate, build: build, current: project, watched: project.watched, stamps: stampFiles(project.watched)}, nil
}

// admit follows the config for one tool call and hands over the notice of
// the changes it has not told yet.
func (w *configWatch) admit(ctx context.Context) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	err := w.follow(ctx)
	if err != nil {
		return "", err
	}
	notice := strings.Join(w.notices, "\n")
	w.notices = nil
	return notice, nil
}

// runtime follows the config for one sync tick.
func (w *configWatch) runtime(ctx context.Context) (*sdd.ProjectRuntime, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	err := w.follow(ctx)
	if err != nil {
		return nil, err
	}
	return w.current.access.runtime, nil
}

func (w *configWatch) served() *servedProject {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.current
}

func (w *configWatch) follow(ctx context.Context) error {
	stamps := stampFiles(w.watched)
	// While broken, the file that broke the build may lie outside the watched
	// set (a project a new pointer names), so every call tries again.
	changed := w.broken != nil || !slices.Equal(stamps, w.stamps)
	w.stamps = stamps
	if changed {
		dir, err := w.locate()
		if err != nil {
			w.broken = err
			return err
		}
		w.broken = nil
		w.track(dir)
	}
	if w.moved != nil {
		to := graphFolderEntries(w.moved.To)
		err := w.moved.Unsettled(to, graphFolderEntries(w.moved.From))
		if err != nil {
			return err
		}
		w.moved = nil
		changed = changed || filepath.Clean(to.Dir) != filepath.Clean(w.current.graphDir)
	}
	if changed {
		err := w.rebuild()
		if err != nil {
			return err
		}
	}
	for _, guard := range w.guards {
		err := guard(ctx, w.current)
		if err != nil {
			return err
		}
	}
	return nil
}

// track records the graph folder the config names now against the one
// served.
func (w *configWatch) track(dir string) {
	served := filepath.Clean(w.current.graphDir)
	switch {
	case filepath.Clean(dir) == served:
		w.moved = nil
	case w.moved == nil:
		w.moved = &model.GraphRelocation{From: w.current.graphDir, To: dir, Entries: w.current.entries}
	default:
		w.moved.To = dir
	}
}

func (w *configWatch) rebuild() error {
	next, err := w.build()
	if err != nil {
		w.broken = err
		return err
	}
	notice := model.FormatConfigNotice(model.ConfigChanges(w.current.layers, next.layers))
	if notice != "" {
		w.notices = append(w.notices, notice)
	}
	w.current = next
	for _, path := range next.watched {
		if !slices.Contains(w.watched, path) {
			w.watched = append(w.watched, path)
		}
	}
	w.stamps = stampFiles(w.watched)
	return nil
}

// graphFolderEntries counts the entry files under dir.
func graphFolderEntries(dir string) model.GraphFolder {
	folder := model.GraphFolder{Dir: dir}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return folder
	}
	folder.Exists = true
	_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr == nil && !entry.IsDir() && model.EntryShaped(rel) {
			folder.Entries++
		}
		return nil
	})
	return folder
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

func (a servedAccess) ResolveDialogue(ctx context.Context, principal sdd.Principal, project sdd.ProjectID) (sdd.Dialogue, error) {
	return a.access().ResolveDialogue(ctx, principal, project)
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
	return s.store().Create(ctx, metadata)
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
