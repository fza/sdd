package mcpapp

import (
	"context"
	"sync"

	"github.com/networkteam/sdd/internal/presenters"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// shellSession is the server's in-memory replay of one durable session, keyed
// by session ID and shared by every connection that presents its handle. It
// holds no authorization: the handle is the capability, the store is the
// authority, and this is a cache (d-cpt-aen). mu serializes the tool calls
// that drive it.
type shellSession struct {
	mu   sync.Mutex
	root *sdd.WorkflowSession
}

// repeats is the no-repeat rule over what the call proves it holds.
func (ss *shellSession) repeats() *presenters.Repeats {
	return &presenters.Repeats{Held: ss.root.HoldsEntry, Anchor: ss.root.ServeEntry}
}

// flushServed records the anchors this call issued into the session ledger.
func (ss *shellSession) flushServed(ctx context.Context, identity sdd.RequestIdentity) error {
	return ss.root.RecordServed(ctx, identity)
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
