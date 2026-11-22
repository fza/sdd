package mcpapp

import (
	"context"
	"slices"
	"sync"

	"github.com/networkteam/sdd/internal/presenters"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/application/types"
)

// shellSession is the server's in-memory replay of one durable session, keyed
// by session ID and shared by every connection that presents its handle. It
// holds no authorization: the handle is the capability, the store is the
// authority, and this is a cache (d-cpt-aen). mu serializes the tool calls
// that drive it.
type shellSession struct {
	mu   sync.Mutex
	root *sdd.WorkflowSession
	// pages holds, per tool, the pages of its last result too large for one,
	// which later calls naming a page serve without running the read again.
	pages map[string][]string
	// playbacks are the serves of the call in flight whose result holds the
	// change they play back, recorded with the served pieces.
	playbacks []*sdd.WorkflowServe
	// pageReads holds, per tool, the full reads of its paged result waiting
	// on the pages not yet served.
	pageReads map[string]pageRead
	// call is the callKey of the call in flight; pagedCall is the callKey of
	// the call before it when that call's answer was paged. repeatsPaged is
	// set while the call in flight repeats it.
	call         string
	pagedCall    string
	repeatsPaged bool
}

type pageRead struct {
	full     []string
	unserved map[int]bool
}

// repeats is the no-repeat rule over what the call holds.
func (ss *shellSession) repeats() *presenters.Repeats {
	return &presenters.Repeats{Held: ss.root.HoldsEntry, Served: ss.root.ServeEntry}
}

// flushServed records the pieces this call served and the playbacks its
// result holds into the session ledger.
func (ss *shellSession) flushServed(ctx context.Context, identity sdd.RequestIdentity) error {
	playbacks := ss.playbacks
	ss.playbacks = nil
	if err := ss.root.RecordPieces(ctx, identity); err != nil {
		return err
	}
	return ss.root.RecordPlayback(ctx, identity, playbacks...)
}

// notePlayback marks serve's playback delivered with the result when the
// result holds its draft lane, keep being how many of its lanes it holds.
func (ss *shellSession) notePlayback(serve *sdd.WorkflowServe, keep int) {
	if serve == nil || !serve.PlaysBack {
		return
	}
	draft := slices.IndexFunc(serve.InstructionLanes, func(lane types.ServeLane) bool { return lane.Name == types.DraftLane })
	if draft >= 0 && draft < keep {
		ss.playbacks = append(ss.playbacks, serve)
	}
}

// sessionCache holds the loaded sessions by ID.
type sessionCache struct {
	mu   sync.Mutex
	byID map[sdd.SessionID]*shellSession
}

func newSessionCache() *sessionCache {
	return &sessionCache{byID: map[sdd.SessionID]*shellSession{}}
}

func (c *sessionCache) get(id sdd.SessionID) *shellSession {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.byID[id]
}

// put caches a freshly loaded session, returning whichever load won when two
// raced for the same ID.
func (c *sessionCache) put(ss *shellSession) *shellSession {
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing := c.byID[ss.root.ID()]; existing != nil {
		return existing
	}
	c.byID[ss.root.ID()] = ss
	return ss
}

func (c *sessionCache) evict(id sdd.SessionID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.byID, id)
}
