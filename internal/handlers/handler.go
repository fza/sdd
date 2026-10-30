// Package handlers is the only place framework Go code is allowed to have
// side effects. Handlers are the write side of CQRS: they accept a domain
// command, do any validation against loaded state, perform IO (git, disk,
// pre-flight via a finder dependency), and return only errors. Results flow
// back through callbacks defined on the command struct.
package handlers

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/pkg/llm"
)

// Reader is the handler-side view of the finder. It bundles every read
// operation handlers need (graph loading, pre-flight, WIP markers, install
// state). Defined here (rather than imported as the concrete *finders.Finder)
// so consumers can substitute fakes in tests — standard "accept interfaces,
// return structs" Go pattern. *finders.Finder satisfies this interface.
type Reader interface {
	// CurrentGraph returns the current graph through the read-side GraphSource
	// seam (a fresh source per call — per-command lifetime, no retained cache).
	// Handlers route through it rather than loading directly so cross-repo
	// assembly reaches the write path for free at the one seam.
	CurrentGraph(dir string) (*model.Graph, error)
	LoadWIPMarkers(graphDir string) ([]*model.WIPMarker, error)
	// Preflight validates an entry against a graph the handler already holds.
	// The graph is passed explicitly (not through the query) because handlers
	// load and mutate it mid-write; the query stays pure intent.
	Preflight(ctx context.Context, g *model.Graph, q query.PreflightQuery) (*query.PreflightResult, error)
	SkillStatus(ctx context.Context, q query.SkillStatusQuery) (*query.SkillStatusResult, error)
	SchemaStatus(ctx context.Context, q query.SchemaStatusQuery) (*query.SchemaStatusResult, error)
	// LocalRepoID and DeclaredDependencies are the root and the step of the
	// dependency walk the refresh drives with its own side effect.
	LocalRepoID() (model.RepoID, error)
	DeclaredDependencies(ctx context.Context, repoID model.RepoID) ([]model.RepoID, error)
}

// Committer performs a git commit of the given paths with the given message.
// Injected so tests can record or no-op commits.
type Committer interface {
	Commit(message string, paths ...string) error
}

// Tracker narrows paths to those git can commit. Init writes agent surfaces
// a repository may ignore, and may prune renders git never tracked. Without
// one, init commits every path it touched.
type Tracker interface {
	Committable(ctx context.Context, paths []string) ([]string, error)
	Uncommitted(ctx context.Context, paths []string) ([]string, error)
}

// Repository commits in, and narrows paths for, one given repository.
type Repository interface {
	Committer
	Tracker
}

// FileMerger three-way merges other into current against base, writing the
// result into current and reporting whether conflict markers were left.
type FileMerger interface {
	MergeFile(ctx context.Context, current, base, other string, markerSize int) (conflicted bool, err error)
}

// MergeDriverInstaller writes the entry merge driver into a repository's
// local config when it has none.
type MergeDriverInstaller interface {
	EnsureMergeDriver(ctx context.Context) error
}

// Brancher performs git branch operations. Injected so tests can fake
// checkout/branch deletion without touching real git.
type Brancher interface {
	Checkout(branch string, create bool) error
	BranchMerged(branch string) bool
	DeleteBranch(branch string, force bool) error
}

// Mover renames a path in the working tree and the git index as one operation
// — the semantics of `git mv`. Injected so tests can fake moves without
// shelling out. Used by the rewrite handler when the target entry changes
// type and its file needs a new on-disk location.
type Mover interface {
	Move(src, dst string) error
}

// ChangeReader reads the working tree against HEAD, so files a failed commit
// left on disk can be found and committed.
type ChangeReader interface {
	// ModifiedFiles lists tracked files under dir whose working copy differs
	// from HEAD, as absolute paths.
	ModifiedFiles(ctx context.Context, dir string) ([]string, error)
	// HeadContent returns the file at path as HEAD records it.
	HeadContent(ctx context.Context, path string) ([]byte, error)
}

// Puller is the git surface for `sdd sync --pull`: a merge-only pull that
// never rewrites the shared graph's history. Injected so tests can fake the
// working-tree state and the pull without shelling out to git.
type Puller interface {
	// IsClean reports whether the working tree has no uncommitted changes
	// (a `git status --porcelain` with empty output).
	IsClean(ctx context.Context) (bool, error)
	// MergePull runs a merge-only pull (`git pull --no-rebase`) and returns
	// git's combined output. A non-nil error means the pull failed.
	MergePull(ctx context.Context) (string, error)
}

// Handler holds injected dependencies shared across command methods.
// Each public method corresponds to one command and lives in its own file
// (handler_new_entry.go, etc.).
type Handler struct {
	graphDir string
	sddDir   string // path to .sdd/ directory; required for commands that write tmp files
	// localConfigPath replaces the in-repo machine-local config layer file;
	// empty resolves it under sddDir.
	localConfigPath string
	reader          Reader
	llmRunner       llm.Runner
	committer       Committer
	tracker         Tracker
	brancher        Brancher
	mover           Mover
	puller          Puller
	pusher          Pusher
	graphPuller     GraphPuller
	fetcher         GraphFetcher
	syncLock        SyncLock
	syncRecord      SyncRecord
	syncMode        model.SyncMode
	changes         ChangeReader
	repos           *repos.Manager
	projectRepos    ProjectRepositories
	repositoryAt    func(dir string) Repository
	fileMerger      FileMerger
	mergeDriverAt   func(dir string) MergeDriverInstaller
	stderr          io.Writer
	now             func() time.Time
	language        string
}

// Options configures a new Handler. Zero-valued fields get sensible defaults.
type Options struct {
	GraphDir string
	SDDDir   string // path to .sdd/ directory; required for commands that write tmp files
	// LocalConfigPath replaces the in-repo machine-local config layer file
	// (--local-config). Empty resolves it under SDDDir.
	LocalConfigPath string
	Reader          Reader
	LLMRunner       llm.Runner
	Committer       Committer
	Tracker         Tracker
	Brancher        Brancher
	Mover           Mover
	Puller          Puller
	Pusher          Pusher
	GraphPuller     GraphPuller
	// GraphFetcher, SyncLock and SyncRecord serve automatic sync rounds; a
	// round without a GraphFetcher reports the graph repository missing.
	GraphFetcher GraphFetcher
	SyncLock     SyncLock
	SyncRecord   SyncRecord
	// SyncMode decides whether a graph commit is pushed before the command
	// returns; empty means manual.
	SyncMode model.SyncMode
	Changes  ChangeReader
	// Repos owns the connected-repos side effects (clone, pull, config
	// writes). Nil means no connected-repos support — repo commands fail
	// loud, and cross-repo cache freshening is skipped.
	Repos *repos.Manager
	// ProjectRepositories and RepositoryAt let a work repository's init set up
	// and refresh its project in a repository of its own. Nil refuses both.
	ProjectRepositories ProjectRepositories
	RepositoryAt        func(dir string) Repository
	// FileMerger merges the rest of an entry file for MergeEntry.
	FileMerger FileMerger
	// MergeDriverAt sets up the entry merge driver in the repository at dir
	// for init. Nil skips the setup.
	MergeDriverAt func(dir string) MergeDriverInstaller
	Stderr        io.Writer
	Now           func() time.Time
	// Language is the graph's configured authoring language locale code
	// (config `language`, empty = English default). Summaries are written
	// in it regardless of the source material's language.
	Language string
}

// New constructs a Handler with the given options.
func New(opts Options) *Handler {
	h := &Handler{
		graphDir:        opts.GraphDir,
		sddDir:          opts.SDDDir,
		localConfigPath: opts.LocalConfigPath,
		reader:          opts.Reader,
		llmRunner:       opts.LLMRunner,
		committer:       opts.Committer,
		tracker:         opts.Tracker,
		brancher:        opts.Brancher,
		mover:           opts.Mover,
		puller:          opts.Puller,
		pusher:          opts.Pusher,
		graphPuller:     opts.GraphPuller,
		fetcher:         opts.GraphFetcher,
		syncLock:        opts.SyncLock,
		syncRecord:      opts.SyncRecord,
		syncMode:        opts.SyncMode,
		changes:         opts.Changes,
		repos:           opts.Repos,
		projectRepos:    opts.ProjectRepositories,
		repositoryAt:    opts.RepositoryAt,
		fileMerger:      opts.FileMerger,
		mergeDriverAt:   opts.MergeDriverAt,
		stderr:          opts.Stderr,
		now:             opts.Now,
		language:        opts.Language,
	}
	if h.stderr == nil {
		h.stderr = os.Stderr
	}
	if h.now == nil {
		h.now = time.Now
	}
	return h
}
