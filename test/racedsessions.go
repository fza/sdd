package test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
)

// RacedSessions is a session store that lands the appends carrying an event
// of Code where another writer of their session cannot see them coming. From
// Hold on such an append waits. Once Land names how many, the session's next
// append by another writer waits until that many are held. They then land in
// turn: one as an append by another writer starts, so the store refuses it,
// the next behind the first load after, which is the refused writer's
// catch-up.
type RacedSessions struct {
	sdd.SessionStore
	Code string

	t       testing.TB
	mu      sync.Mutex
	session sdd.SessionID
	held    []*heldAppend
	owed    int
	chasing bool
	waiting chan struct{}
	waited  bool
}

type heldAppend struct {
	data sdd.SessionAppend
	done chan struct{}
	next uint64
	err  error
}

// NewRacedSessions returns store with the appends carrying code raced; what
// is still held when the test ends fails.
func NewRacedSessions(t testing.TB, store sdd.SessionStore, code string) *RacedSessions {
	s := &RacedSessions{SessionStore: store, Code: code, t: t, waiting: make(chan struct{})}
	t.Cleanup(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.session = ""
		for _, held := range s.held {
			held.err = errors.New("the test ended")
			close(held.done)
		}
		s.held = nil
	})
	return s
}

// Hold has every append to session that carries Code wait from here on.
func (s *RacedSessions) Hold(session sdd.SessionID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session = session
}

// Land lands n held appends around the session's next appends by another
// writer.
func (s *RacedSessions) Land(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.owed = n
}

// Waiting is closed once an append by another writer waits for the appends
// Land named.
func (s *RacedSessions) Waiting() <-chan struct{} { return s.waiting }

// Held is how many appends wait.
func (s *RacedSessions) Held() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.held)
}

func (s *RacedSessions) Append(ctx context.Context, id sdd.SessionID, expected uint64, data sdd.SessionAppend) (uint64, error) {
	s.mu.Lock()
	if id != s.session {
		s.mu.Unlock()
		return s.SessionStore.Append(ctx, id, expected, data)
	}
	if slices.ContainsFunc(data.Events, func(event sdd.StoredEvent) bool { return event.Code == s.Code }) {
		held := &heldAppend{data: data, done: make(chan struct{})}
		s.held = append(s.held, held)
		s.mu.Unlock()
		<-held.done
		return held.next, held.err
	}
	if s.owed > 0 {
		s.awaitHeld()
		s.land(ctx)
		s.chasing = true
	}
	s.mu.Unlock()
	return s.SessionStore.Append(ctx, id, expected, data)
}

func (s *RacedSessions) Load(ctx context.Context, id sdd.SessionID) (sdd.StoredSession, error) {
	stored, err := s.SessionStore.Load(ctx, id)
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == s.session && s.chasing {
		s.chasing = false
		if s.owed > 0 {
			s.land(ctx)
		}
	}
	return stored, err
}

// awaitHeld waits, the lock held on entry and exit, until every append Land
// named is held.
func (s *RacedSessions) awaitHeld() {
	if !s.waited {
		s.waited = true
		close(s.waiting)
	}
	deadline := time.Now().Add(roundWait)
	for len(s.held) < s.owed {
		if time.Now().After(deadline) {
			s.t.Errorf("%d appends carrying %s should wait to land, %d do", s.owed, s.Code, len(s.held))
			s.owed = len(s.held)
			return
		}
		s.mu.Unlock()
		time.Sleep(time.Millisecond)
		s.mu.Lock()
	}
}

// land appends the longest-held append on the version and metadata the
// store holds now, as its writer's retry would.
func (s *RacedSessions) land(ctx context.Context) {
	if len(s.held) == 0 {
		return
	}
	held := s.held[0]
	s.held = s.held[1:]
	s.owed--
	stored, err := s.SessionStore.Load(ctx, s.session)
	if err == nil {
		metadata := stored.Metadata
		held.data.Metadata = &metadata
		held.next, err = s.SessionStore.Append(ctx, s.session, stored.Version, held.data)
	}
	held.err = err
	close(held.done)
}
