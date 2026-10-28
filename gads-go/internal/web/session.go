package web

import (
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
}

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
}

func newSessionStore(idle time.Duration) *sessionStore {
	return &sessionStore{data: map[string]*Session{}, idle: idle}
}

func (st *sessionStore) get(id string) *Session {
	st.mu.Lock()
	defer st.mu.Unlock()
	s := st.data[id]
	if s == nil {
		return nil
	}
	if st.expired(s, time.Now()) {
		delete(st.data, id)
		return nil
	}
	s.used = time.Now()
	return s
}

func (st *sessionStore) put(s *Session) {
	st.mu.Lock()
	defer st.mu.Unlock()
	now := time.Now()
	s.used = now
	st.data[s.ID] = s
	// Expired sessions are swept at most once per idle period
	if st.idle > 0 && now.Sub(st.pruned) > st.idle {
		for id, x := range st.data {
			if st.expired(x, now) {
				delete(st.data, id)
			}
		}
		st.pruned = now
	}
}

func (st *sessionStore) expired(s *Session, now time.Time) bool {
	return st.idle > 0 && now.Sub(s.used) > st.idle
}

func (st *sessionStore) remove(id string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.data, id)
}

func newSessionID() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
