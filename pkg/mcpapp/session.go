package mcpapp

import (
	"context"
	"sync"

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
	// ask marks the call in flight as one that starts, resumes or advances
	// the session, so its response ends with a question.
	ask bool
}

// flushServed records the pieces this call served into the session ledger.
func (ss *shellSession) flushServed(ctx context.Context, identity sdd.RequestIdentity) error {
	return ss.root.RecordPieces(ctx, identity)
}

// question is the line a response of the call in flight ends with: the text
// question (20261121-230157-d-tac-ym8) when the call starts, resumes or
// advances the session, else empty. Called once the response is composed.
func (ss *shellSession) question(ctx context.Context, identity sdd.RequestIdentity) (string, error) {
	if !ss.ask {
		return "", nil
	}
	if err := ss.flushServed(ctx, identity); err != nil {
		return "", err
	}
	return ss.root.Question(ctx, identity)
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
