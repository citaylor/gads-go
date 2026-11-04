package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
)

// sessionTable is the table of the DBIC session engine (as
// Dancer2::Session::DBIC: the session's ID and its data), with the time it
// was last used, for the idle expiry. It is not part of GADS's schema:
// PersistSessions creates it. The ID column holds a hash of the session's
// ID (rowKey), so that whoever reads the table (or a backup of it) cannot
// take over the sessions.
const sessionTable = `CREATE TABLE IF NOT EXISTS "sessions" (
  "id" varchar(255) NOT NULL PRIMARY KEY,
  "session_data" text NOT NULL,
  "last_active" bigint NOT NULL
)`

// sessionTouch is how often the time of use of an unchanged session is
// written
const sessionTouch = time.Minute

// sessionBackend keeps sessions over restarts (the DBIC session engine)
type sessionBackend struct {
	// The connection is the backend's own, so that a session is written
	// whatever the request's transaction does (as Perl's session engines),
	// and one statement runs at a time on it
	mu sync.Mutex
	d  *db.DB
}

// PersistSessions keeps the sessions in the table "sessions" of d (session:
// DBIC), so that they survive a restart. d is a connection of its own.
func (s *Server) PersistSessions(d *db.DB) error {
	if _, err := d.TryExec(sessionTable); err != nil {
		return err
	}
	s.sessions.backend = &sessionBackend{d: d}
	s.sessions.pruneBackend(time.Now())
	return nil
}

// rowKey is the key of a session's row: the SHA-256 of its ID
func rowKey(id string) string {
	h := sha256.Sum256([]byte(id))
	return hex.EncodeToString(h[:])
}

// load reads a session: nil if there is none
func (b *sessionBackend) load(id string) (*Session, error) {
	b.mu.Lock()
	rows, err := b.d.TryQuery(`SELECT "session_data", "last_active" FROM "sessions" WHERE "id" = ?`, rowKey(id))
	b.mu.Unlock()
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	data, _ := db.AsString(rows[0]["session_data"])
	used, _ := db.AsInt64(rows[0]["last_active"])
	values, err := decodeSession(data)
	if err != nil {
		return nil, fmt.Errorf("session %.8s...: %w", id, err)
	}
	return &Session{ID: id, Data: values, used: time.Unix(used, 0), saved: data, savedID: id,
		savedAt: time.Unix(used, 0)}, nil
}

func (b *sessionBackend) save(id, data string, used time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, err := b.d.TryExec(`INSERT INTO "sessions" ("id", "session_data", "last_active") VALUES (?, ?, ?)
 ON CONFLICT ("id") DO UPDATE SET "session_data" = excluded."session_data", "last_active" = excluded."last_active"`,
		rowKey(id), data, used.Unix())
	return err
}

func (b *sessionBackend) remove(id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, err := b.d.TryExec(`DELETE FROM "sessions" WHERE "id" = ?`, rowKey(id))
	return err
}

func (b *sessionBackend) prune(before time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, err := b.d.TryExec(`DELETE FROM "sessions" WHERE "last_active" < ?`, before.Unix())
	return err
}

// ---------------------------------------------------------------------
// Encoding: the values of a session keep their Go types, which the routes
// read back with type assertions. Each value is [type, value].
// ---------------------------------------------------------------------

func encodeSession(data map[string]any) (string, error) {
	out := make(map[string]any, len(data))
	for k, v := range data {
		e, err := encodeSessionValue(v)
		if err != nil {
			return "", fmt.Errorf("session value %s: %w", k, err)
		}
		out[k] = e
	}
	b, err := json.Marshal(out)
	return string(b), err
}

func encodeSessionValue(v any) (any, error) {
	switch x := v.(type) {
	case nil:
		return []any{"z"}, nil
	case string:
		return []any{"s", x}, nil
	case bool:
		return []any{"b", x}, nil
	case int:
		return []any{"n", strconv.Itoa(x)}, nil
	case int64:
		return []any{"i", strconv.FormatInt(x, 10)}, nil
	case float64:
		return []any{"f", strconv.FormatFloat(x, 'g', -1, 64)}, nil
	case time.Time:
		return []any{"t", x.Format(time.RFC3339Nano), x.Location().String()}, nil
	case *ttDateTime:
		return []any{"dt", x.t.Format(time.RFC3339Nano), x.t.Location().String()}, nil
	case []string:
		return []any{"ls", x}, nil
	case []*Message:
		return []any{"msg", x}, nil
	case []any:
		l := make([]any, len(x))
		for i, e := range x {
			var err error
			if l[i], err = encodeSessionValue(e); err != nil {
				return nil, err
			}
		}
		return []any{"l", l}, nil
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, e := range x {
			var err error
			if m[k], err = encodeSessionValue(e); err != nil {
				return nil, err
			}
		}
		return []any{"m", m}, nil
	}
	return nil, fmt.Errorf("a %T cannot be stored", v)
}

func decodeSession(s string) (map[string]any, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil, err
	}
	out := make(map[string]any, len(raw))
	for k, r := range raw {
		v, err := decodeSessionValue(r)
		if err != nil {
			return nil, fmt.Errorf("session value %s: %w", k, err)
		}
		out[k] = v
	}
	return out, nil
}

func decodeSessionValue(r json.RawMessage) (any, error) {
	var parts []json.RawMessage
	if err := json.Unmarshal(r, &parts); err != nil || len(parts) == 0 {
		return nil, fmt.Errorf("bad value %.40s", r)
	}
	var tag string
	if err := json.Unmarshal(parts[0], &tag); err != nil {
		return nil, err
	}
	if tag == "z" {
		return nil, nil
	}
	if len(parts) < 2 {
		return nil, fmt.Errorf("bad value %.40s", r)
	}
	str := func() (string, error) {
		var s string
		err := json.Unmarshal(parts[1], &s)
		return s, err
	}
	switch tag {
	case "s":
		return str()
	case "b":
		var b bool
		err := json.Unmarshal(parts[1], &b)
		return b, err
	case "n", "i", "f":
		s, err := str()
		if err != nil {
			return nil, err
		}
		switch tag {
		case "n":
			return strconv.Atoi(s)
		case "i":
			return strconv.ParseInt(s, 10, 64)
		}
		return strconv.ParseFloat(s, 64)
	case "t", "dt":
		s, err := str()
		if err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return nil, err
		}
		if len(parts) > 2 {
			var name string
			if json.Unmarshal(parts[2], &name) == nil {
				if loc, err := time.LoadLocation(name); err == nil {
					t = t.In(loc)
				}
			}
		}
		if tag == "dt" {
			return &ttDateTime{t: t}, nil
		}
		return t, nil
	case "ls":
		var l []string
		err := json.Unmarshal(parts[1], &l)
		return l, err
	case "msg":
		var l []*Message
		err := json.Unmarshal(parts[1], &l)
		return l, err
	case "l":
		var raw []json.RawMessage
		if err := json.Unmarshal(parts[1], &raw); err != nil {
			return nil, err
		}
		l := make([]any, len(raw))
		for i, e := range raw {
			var err error
			if l[i], err = decodeSessionValue(e); err != nil {
				return nil, err
			}
		}
		return l, nil
	case "m":
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(parts[1], &raw); err != nil {
			return nil, err
		}
		m := make(map[string]any, len(raw))
		for k, e := range raw {
			var err error
			if m[k], err = decodeSessionValue(e); err != nil {
				return nil, err
			}
		}
		return m, nil
	}
	return nil, fmt.Errorf("unknown type %q", tag)
}
