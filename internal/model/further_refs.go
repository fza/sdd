package model

import (
	"slices"
	"time"

	"github.com/networkteam/sdd/pkg/application/types"
)

type (
	UnavailableRefKind = types.UnavailableRefKind
	UnavailableRef     = types.UnavailableRef
)

const (
	RefNotConnected      = types.RefNotConnected
	RefMissingAfterFetch = types.RefMissingAfterFetch
	RefFetchDeferred     = types.RefFetchDeferred
)

// RefMissRetry is how long a recorded miss holds off the next fetch for its
// entry while the clone stands still.
const RefMissRetry = 24 * time.Hour

// RefMiss records a completed fetch of Repo whose clone, at Commit, still
// lacked EntryID (20261113-235859-d-cpt-l3m).
type RefMiss struct {
	Repo    RepoID    `json:"repo"`
	EntryID string    `json:"entry_id"`
	Commit  string    `json:"commit"`
	At      time.Time `json:"at"`
}

// Retry reports whether a fetch for the missed entry may run again: once the
// clone's commit moved or RefMissRetry passed.
func (m RefMiss) Retry(commit string, now time.Time) bool {
	return commit != m.Commit || !now.Before(m.At.Add(RefMissRetry))
}

// FurtherRef is what a FurtherResolver answers for one entry of a repo beyond
// the direct dependencies.
type FurtherRef struct {
	// Graph is the repo's clone graph; it holds the entry unless Unavailable
	// is set.
	Graph *Graph
	// CloneFetched is when the clone last fetched, the age a resolved ref
	// shows.
	CloneFetched time.Time
	// Fetched reports that a fetch of the repo completed in this call.
	Fetched     bool
	Unavailable *UnavailableRef
}

// FurtherResolver resolves entryID of repoID, a repo a direct dependency's
// ref names beyond the home's direct dependencies. fetched reports that a
// fetch of repoID already completed during this read, so the resolver fetches
// at most once per repo and read.
type FurtherResolver func(repoID RepoID, entryID string, fetched bool) (FurtherRef, error)

// furtherRepo is what one read knows of a repo beyond the direct
// dependencies: its clone graph, whether a fetch completed, and the
// unavailability that holds for every entry (not connected, fetch deferred).
type furtherRepo struct {
	graph   *Graph
	ran     bool
	blocked *UnavailableRef
}

type furtherEntry struct {
	graph       *Graph
	unavailable *UnavailableRef
	err         error
}

// ResolveFurther wires the resolver refs into repos beyond the direct
// dependencies go through. Only refs a direct dependency's graph holds reach
// it: the home's own refs stay within its declarations, and a further-out
// graph's cross-repo refs resolve no further.
func (m *MultiGraph) ResolveFurther(r FurtherResolver) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.further = r
}

// ObserveFurther has observe told every further-out ref a read resolves:
// unavailable is nil once it resolved.
func (m *MultiGraph) ObserveFurther(observe func(ref string, unavailable *UnavailableRef)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.observe = observe
}

// furtherFrom reports whether a cross-repo ID from graph from into repoID
// resolves beyond the direct dependencies.
func (m *MultiGraph) furtherFrom(from *Graph, repoID RepoID) bool {
	if m == nil || m.further == nil || from == m.Local || slices.Contains(m.deps, repoID) {
		return false
	}
	return slices.Contains(m.deps, from.repoID)
}

// resolveFurther resolves entryID of repoID through the resolver, at most
// once per entry and read.
func (m *MultiGraph) resolveFurther(repoID RepoID, entryID string) furtherEntry {
	key := CrossRepoID(repoID, entryID)
	m.mu.Lock()
	defer m.mu.Unlock()
	if known, ok := m.furtherEntries[key]; ok {
		return known
	}
	state := m.furtherRepos[repoID]
	if state == nil {
		state = &furtherRepo{}
		m.furtherRepos[repoID] = state
	}
	entry := m.lookupFurther(state, repoID, entryID)
	m.furtherEntries[key] = entry
	if m.observe != nil && entry.err == nil {
		m.observe(key, entry.unavailable)
	}
	if entry.unavailable != nil {
		m.unavailable = append(m.unavailable, *entry.unavailable)
	}
	return entry
}

func (m *MultiGraph) lookupFurther(state *furtherRepo, repoID RepoID, entryID string) furtherEntry {
	if state.graph != nil && state.graph.ByID[entryID] != nil {
		return furtherEntry{graph: state.graph}
	}
	if state.blocked != nil {
		blocked := *state.blocked
		blocked.EntryID = entryID
		return furtherEntry{unavailable: &blocked}
	}
	result, err := m.further(repoID, entryID, state.ran)
	if err != nil {
		return furtherEntry{err: err}
	}
	state.ran = state.ran || result.Fetched
	if result.Graph != nil {
		result.Graph.multi = m
		result.Graph.repoID = repoID
		result.Graph.further = true
		result.Graph.cloneFetched = result.CloneFetched
		state.graph = result.Graph
	}
	if u := result.Unavailable; u != nil {
		if u.Kind != RefMissingAfterFetch {
			state.blocked = u
		}
		return furtherEntry{unavailable: u}
	}
	if state.graph == nil || state.graph.ByID[entryID] == nil {
		return furtherEntry{}
	}
	return furtherEntry{graph: state.graph}
}

// UnavailableRef returns why a cross-repo ID a read met beyond the direct
// dependencies stayed unresolved.
func (g *Graph) UnavailableRef(id string) *UnavailableRef {
	m := g.multi
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.furtherEntries[id].unavailable
}

// UnavailableRefs lists every ref beyond the direct dependencies the reads
// over this assembly could not resolve, once each, in the order met.
func (g *Graph) UnavailableRefs() []UnavailableRef {
	m := g.multi
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.unavailable)
}

// CloneFetched is when the clone a graph beyond the direct dependencies was
// read from last fetched; ok is false for every other graph.
func (g *Graph) CloneFetched() (fetched time.Time, ok bool) {
	return g.cloneFetched, g.further
}
