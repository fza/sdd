package mcpapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	// pending are the content hashes of blocks the call in flight served in
	// full, recorded into the session ledger when the call's result is
	// composed; seen dedups within the call.
	pending []string
	seen    map[string]bool
	// inFull consults only this call, never the ledger (Options.ServeInFull).
	inFull bool
}

// servedBefore reports whether the session's consumer already holds a block
// with these exact bytes — from the ledger, or earlier in this call — and
// marks it served when not.
func (ss *shellSession) servedBefore(block string) bool {
	hash := blockHash(block)
	if ss.seen[hash] || (!ss.inFull && ss.root.ServedBefore(hash)) {
		return true
	}
	if ss.seen == nil {
		ss.seen = map[string]bool{}
	}
	ss.seen[hash] = true
	ss.pending = append(ss.pending, hash)
	return false
}

// heldEntryKey is the served-set key of an entry served in full: the
// no-repeat rule's unit (20261109-004506-d-cpt-udz, R14). Bodies are
// immutable, so the ID alone keys it.
func heldEntryKey(id string) string { return "entry-full:" + id }

// holdsEntry reports whether the consumer already holds the entry in full,
// without marking it.
func (ss *shellSession) holdsEntry(id string) bool {
	hash := blockHash(heldEntryKey(id))
	return ss.root.ServedBefore(hash) || ss.seen[hash]
}

// repeats is the no-repeat rule over what the consumer already holds.
func (ss *shellSession) repeats() *presenters.Repeats {
	return &presenters.Repeats{Held: ss.holdsEntry}
}

// holdEntries marks entries served in full by the call in flight.
func (ss *shellSession) holdEntries(ids []string) {
	for _, id := range ids {
		ss.servedBefore(heldEntryKey(id))
	}
}

// flushServed records what this call served in full into the session ledger.
func (ss *shellSession) flushServed(ctx context.Context, identity sdd.RequestIdentity) error {
	hashes := ss.pending
	ss.pending, ss.seen = nil, nil
	return ss.root.RecordServed(ctx, identity, hashes)
}

func blockHash(block string) string {
	sum := sha256.Sum256([]byte(block))
	return hex.EncodeToString(sum[:])
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
