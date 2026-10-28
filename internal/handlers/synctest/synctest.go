// Package synctest provides fakes of the git surface and the sync state that
// graph sync runs on, for tests of the sync handlers and finders.
package synctest

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/networkteam/sdd/internal/model"
)

// ErrForbidden is what a Repo returns from an operation its test forbids.
var ErrForbidden = errors.New("operation forbidden by the test")

// Repo is a scripted graph repository. Every operation is logged in Calls;
// one named in Forbid fails with ErrForbidden.
type Repo struct {
	mu sync.Mutex

	NoRepo, NoRemote bool
	Upstream         string
	FetchErr         error
	PushErr          error
	// Ahead and GraphAhead count commits ahead of upstream; Behind and
	// GraphBehind the ones upstream holds ahead.
	Ahead, GraphAhead, Behind, GraphBehind int
	Changed                                []string
	Dirty                                  bool
	Conflicts                              []string
	Commits                                map[string]string
	Contained                              map[string]bool
	Forbid                                 []string
	Calls                                  []string
}

func (r *Repo) call(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Calls = append(r.Calls, name)
	if slices.Contains(r.Forbid, name) {
		return ErrForbidden
	}
	return nil
}

// Called reports whether name ran.
func (r *Repo) Called(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Contains(r.Calls, name)
}

func (r *Repo) InRepo(context.Context) bool    { return r.call("in-repo") == nil && !r.NoRepo }
func (r *Repo) HasRemote(context.Context) bool { return r.call("has-remote") == nil && !r.NoRemote }

func (r *Repo) UpstreamRef(context.Context) (string, error) {
	return r.Upstream, r.call("upstream")
}

func (r *Repo) Fetch(context.Context) error {
	if err := r.call("fetch"); err != nil {
		return err
	}
	return r.FetchErr
}

func (r *Repo) CountCommits(_ context.Context, rangeSpec, pattern string) (int, error) {
	if err := r.call("count"); err != nil {
		return 0, err
	}
	switch {
	case rangeSpec == "HEAD..@{u}" && pattern == "":
		return r.Behind, nil
	case rangeSpec == "HEAD..@{u}":
		return r.GraphBehind, nil
	case pattern == "":
		return r.Ahead, nil
	default:
		return r.GraphAhead, nil
	}
}

func (r *Repo) ChangedPaths(context.Context, string, string) ([]string, error) {
	return r.Changed, r.call("changed-paths")
}

func (r *Repo) RepoPath(_ context.Context, path string) (string, error) {
	return path, r.call("repo-path")
}

func (r *Repo) IsClean(context.Context) (bool, error) { return !r.Dirty, r.call("is-clean") }

func (r *Repo) MergeTreePredict(context.Context, string, string) ([]string, error) {
	return r.Conflicts, r.call("merge-tree")
}

func (r *Repo) Merge(context.Context, string, string) error { return r.call("merge") }

func (r *Repo) Push(context.Context) error {
	if err := r.call("push"); err != nil {
		return err
	}
	return r.PushErr
}

func (r *Repo) RevParse(_ context.Context, ref string) (string, error) {
	if err := r.call("rev-parse"); err != nil {
		return "", err
	}
	if commit, ok := r.Commits[ref]; ok {
		return commit, nil
	}
	return ref, nil
}

func (r *Repo) Contains(_ context.Context, commit string) (bool, error) {
	return r.Contained[commit], r.call("contains")
}

// Lock is an in-process sync lock; Held makes TryLock find it taken.
type Lock struct {
	Held  bool
	Taken int
}

func (l *Lock) TryLock(context.Context) (func(), error) {
	if l.Held {
		return nil, nil
	}
	l.Taken++
	return func() {}, nil
}

func (l *Lock) Lock(context.Context) (func(), error) {
	l.Taken++
	return func() {}, nil
}

// Record is an in-memory blocked-sync record.
type Record struct {
	mu      sync.Mutex
	Current *model.SyncBlocked
}

func (r *Record) Load() (*model.SyncBlocked, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Current == nil {
		return nil, nil
	}
	current := *r.Current
	return &current, nil
}

func (r *Record) Update(change func(*model.SyncBlocked) *model.SyncBlocked) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var current *model.SyncBlocked
	if r.Current != nil {
		copied := *r.Current
		current = &copied
	}
	r.Current = change(current)
	return nil
}
