package web

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

// Session is a Dancer2 session (Dancer2::Session::Simple stores sessions
// in memory).
type Session struct {
	ID   string
	Data map[string]any
	// used is when the session was last read or written
	used time.Time
	// lock is held by the request of the session being dispatched (a
	// channel, so that a request can stop waiting for it): see acquire
	lock     chan struct{}
	lockOnce sync.Once
	// saved is the data as last written to the backend, under savedID, at
	// savedAt
	saved, savedID string
	savedAt        time.Time
}

// acquire takes the request lock of the session, waiting until ctx is
// done, and is whether it was taken
func (s *Session) acquire(ctx context.Context) bool {
	s.lockOnce.Do(func() { s.lock = make(chan struct{}, 1) })
	select {
	case s.lock <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

// release releases the request lock taken by acquire
func (s *Session) release() { <-s.lock }

// Get reads a session value
func (s *Session) Get(k string) any { return s.Data[k] }

// Set writes a session value
func (s *Session) Set(k string, v any) { s.Data[k] = v }

// sessionStore holds sessions in memory. With an idle timeout, sessions
// not used for that long are dropped.
type sessionStore struct {
	mu     sync.Mutex
	data   map[string]*Session
	idle   time.Duration
	pruned time.Time
	// backend, if set, keeps the sessions over restarts: those in memory
	// are its cache, which a session missing from memory is read into
	backend *sessionBackend
	// logError reports a failure of the backend
	logError func(msg string, err error)
}

func newSessionStore(idle time.Duration) *sessionStore {
	return &sessionStore{data: map[string]*Session{}, idle: idle}
}

func (st *sessionStore) get(id string) *Session {
	if st.backend != nil && !st.refresh(id) {
		return nil
	}
	s := st.lookup(id)
	if s == nil {
		return nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.expired(s, time.Now()) {
		if st.data[id] == s {
			delete(st.data, id)
		}
		return nil
	}
	s.used = time.Now()
	return s
}

// peek is the session of an ID that has not expired, without marking it
// used
func (st *sessionStore) peek(id string) *Session {
	s := st.lookup(id)
	if s == nil {
		return nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.expired(s, time.Now()) {
		return nil
	}
	return s
}

func (st *sessionStore) put(s *Session) {
	st.mu.Lock()
	now := time.Now()
	s.used = now
	st.data[s.ID] = s
	// Expired sessions are swept at most once per idle period
	prune := st.idle > 0 && now.Sub(st.pruned) > st.idle
	if prune {
		for id, x := range st.data {
			if st.expired(x, now) {
				delete(st.data, id)
			}
		}
		st.pruned = now
	}
	st.mu.Unlock()
	if prune {
		st.pruneBackend(now)
	}
}

// lookup is the session of an ID in memory, otherwise read from the
// backend. The backend is read without the store's lock, so that a slow
// database holds up only the requests of that session.
func (st *sessionStore) lookup(id string) *Session {
	st.mu.Lock()
	s := st.data[id]
	st.mu.Unlock()
	if s != nil || st.backend == nil {
		return s
	}
	loaded, err := st.backend.load(id)
	if err != nil {
		st.fail("reading a session failed", err)
	}
	if loaded == nil {
		return nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	// Another request may have read it meanwhile: there is one Session,
	// whose lock its requests share
	if cur := st.data[id]; cur != nil {
		return cur
	}
	st.data[id] = loaded
	return loaded
}

// refresh rereads a session in memory from the backend, as each request
// reads its session (Dancer2's stored engines): another process may have
// changed or deleted it. It is whether the session may exist. The
// session's request lock is held.
func (st *sessionStore) refresh(id string) bool {
	st.mu.Lock()
	s := st.data[id]
	st.mu.Unlock()
	if s == nil {
		return true
	}
	stored, err := st.backend.load(id)
	if err != nil {
		st.fail("reading a session failed", err)
		return true
	}
	if stored == nil {
		st.mu.Lock()
		if st.data[id] == s {
			delete(st.data, id)
		}
		st.mu.Unlock()
		return false
	}
	if stored.saved != s.saved {
		s.Data, s.saved = stored.Data, stored.saved
	}
	st.mu.Lock()
	if stored.used.After(s.used) {
		s.used = stored.used
	}
	st.mu.Unlock()
	return true
}

// pruneBackend deletes the stored sessions that have expired
func (st *sessionStore) pruneBackend(now time.Time) {
	if st.backend != nil && st.idle > 0 {
		if err := st.backend.prune(now.Add(-st.idle)); err != nil {
			st.fail("deleting expired sessions failed", err)
		}
	}
}

// save writes a session to the backend at the end of its request, if it
// is in the store and its data changed (or it has not been written for
// sessionTouch, so that its idle expiry follows its use). The session's
// request lock is held.
func (st *sessionStore) save(s *Session) {
	if st.backend == nil || s == nil {
		return
	}
	st.mu.Lock()
	cur := st.data[s.ID]
	used := s.used
	st.mu.Unlock()
	if cur != s {
		return
	}
	data, err := encodeSession(s.Data)
	if err != nil {
		st.fail("a session could not be saved", err)
		return
	}
	if s.savedID == s.ID && data == s.saved && used.Sub(s.savedAt) < sessionTouch {
		return
	}
	if s.savedID != "" && s.savedID != s.ID {
		// The ID changed (change_session_id)
		if err := st.backend.remove(s.savedID); err != nil {
			st.fail("deleting a session failed", err)
		}
	}
	if err := st.backend.save(s.ID, data, used); err != nil {
		st.fail("saving a session failed", err)
		return
	}
	s.saved, s.savedID, s.savedAt = data, s.ID, used
}

func (st *sessionStore) fail(msg string, err error) {
	if st.logError != nil {
		st.logError(msg, err)
	}
}

func (st *sessionStore) expired(s *Session, now time.Time) bool {
	return st.idle > 0 && now.Sub(s.used) > st.idle
}

func (st *sessionStore) remove(id string) {
	st.mu.Lock()
	s := st.data[id]
	delete(st.data, id)
	st.mu.Unlock()
	if st.backend == nil {
		return
	}
	// The stored row is that of the ID the session was saved under
	saved := id
	if s != nil && s.savedID != "" {
		saved = s.savedID
	}
	if err := st.backend.remove(saved); err != nil {
		st.fail("deleting a session failed", err)
	}
	if s != nil {
		s.savedID = ""
	}
}

func newSessionID() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
