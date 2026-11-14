package application

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/application/types"
)

// UnavailableRef is a ref a direct dependency holds into a project beyond the
// home's direct dependencies that stays unresolved, with its kind, reason and
// who must act (20261113-235859-d-cpt-l3m).
type UnavailableRef = types.UnavailableRef

// FurtherRefs resolves the refs a direct dependency holds into projects
// beyond the home's direct dependencies (20261113-235859-d-cpt-l3m).
type FurtherRefs interface {
	// ResolveFurther resolves entryID of repo: the runtime over repo's clone
	// when it holds the entry, with when that clone last fetched, else one
	// fetch on a miss unless fetched reports that a fetch of repo already
	// completed during this read.
	ResolveFurther(ctx context.Context, repo RepoID, entryID string, fetched bool) (FurtherResolution, error)
}

// FurtherResolution is what FurtherRefs answers for one entry.
type FurtherResolution struct {
	// Runtime serves the repo's clone; nil while the entry is unavailable.
	Runtime      *ProjectRuntime
	CloneFetched time.Time
	// Fetched reports that this call completed a fetch of the repo.
	Fetched     bool
	Unavailable *UnavailableRef
}

// furtherResolver adapts runtime's FurtherRefs to the cross-graph assembly,
// reading each resolved clone's snapshot once.
func furtherResolver(ctx context.Context, runtime *ProjectRuntime) model.FurtherResolver {
	graphs := map[ProjectID]*model.Graph{}
	return func(repoID model.RepoID, entryID string, fetched bool) (model.FurtherRef, error) {
		resolution, err := runtime.options.Further.ResolveFurther(ctx, repoID, entryID, fetched)
		if err != nil {
			return model.FurtherRef{}, err
		}
		ref := model.FurtherRef{CloneFetched: resolution.CloneFetched, Fetched: resolution.Fetched, Unavailable: resolution.Unavailable}
		if resolution.Runtime == nil {
			return ref, nil
		}
		project := resolution.Runtime.options.Project.ID
		if graphs[project] == nil || resolution.Fetched {
			selected, err := acquireSnapshotForReadBranch(ctx, resolution.Runtime, "")
			if err != nil {
				return model.FurtherRef{}, err
			}
			graphs[project] = model.NewGraph(append([]*model.Entry(nil), selected.snapshot.graph.Entries...))
			selected.releaseInto(&err)
			if err != nil {
				return model.FurtherRef{}, err
			}
		}
		ref.Graph = graphs[project]
		return ref, nil
	}
}

// furtherNotices holds, per ref, the unavailability the runtime's reads last
// met, for the sync notice; a ref that resolves again drops out.
type furtherNotices struct {
	mu   sync.Mutex
	refs map[string]UnavailableRef
}

func (f *furtherNotices) note(ref string, unavailable *UnavailableRef) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if unavailable == nil {
		delete(f.refs, ref)
		return
	}
	if f.refs == nil {
		f.refs = map[string]UnavailableRef{}
	}
	f.refs[ref] = *unavailable
}

func (f *furtherNotices) notice() string {
	if f == nil {
		return ""
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	lines := make([]string, 0, len(f.refs))
	for _, ref := range slices.Sorted(maps.Keys(f.refs)) {
		u := f.refs[ref]
		line := fmt.Sprintf("ref to %s unavailable (%s): %s; who must act: %s", ref, u.Kind, u.Reason(), u.Actor())
		if u.Kind == types.RefNotConnected && !u.At.IsZero() {
			line += fmt.Sprintf("; seen %s", u.At.Local().Format(time.DateTime))
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
