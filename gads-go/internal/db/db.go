// Package db provides database access for the GADS engine using the same
// schema as the original Perl application. It offers nested transaction
// guards with DBIx::Class txn_scope_guard semantics, savepoints, and helpers
// that keep stored values byte-identical to those written by GADS.
package db

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

//go:embed sql_sqlite.sql
var sqliteSchema string

//go:embed sql_postgres.sql
var postgresSchema string

// Dialect identifies the SQL dialect in use.
type Dialect int

const (
	SQLite Dialect = iota
	Postgres
)

// DB is a single database session. GADS (via DBIx::Class) uses a single
// connection per process, which is mirrored here.
type DB struct {
	raw     *sql.DB
	conn    *sql.Conn
	Dialect Dialect
	depth   int
	svp     int
	// Trace, if set, receives every SQL statement executed
	Trace func(query string, args []any)
	// FullErrors gives PostgreSQL errors in full (over several lines), as
	// GADS reports them once its web application has set the schema's
	// exception_action to panic with them
	FullErrors bool
	// WriteCheck, if set, is called with the table and statement of every
	// INSERT, UPDATE and DELETE before it runs (it may panic to refuse it)
	WriteCheck func(table, query string)
}

// writeTableRe finds the table written by a statement
var writeTableRe = regexp.MustCompile(`(?is)^\s*(?:INSERT\s+INTO|UPDATE|DELETE\s+FROM)\s+"?([A-Za-z_][A-Za-z0-9_]*)"?`)

// checkWrite calls WriteCheck for a statement that writes a table
func (d *DB) checkWrite(q string) {
	if d.WriteCheck == nil {
		return
	}
	if m := writeTableRe.FindStringSubmatch(q); m != nil {
		d.WriteCheck(m[1], q)
	}
}

// Error is a database error (equivalent of a DBI exception).
type Error struct {
	Query string
	Err   error
	// Populate is set for an error of Populate: DBIx::Class gives the error
	// of a bulk insert over several lines ("execute_for_fetch() aborted
	// with ... at populate slice:" and the row), so GADS reports it without
	// a message
	Populate bool
	// Full is FullErrors of the database at the time of the error
	Full bool
}

// Error is the message of the error. A PostgreSQL error is given as GADS
// reports a DBI exception of DBD::Pg (by Log::Report's decoding of
// DBIx::Class exceptions): "DBI Exception: DBD::Pg::st execute failed:
// ERROR:  <message> [for Statement "<sql>"]", or nothing if libpq gives the
// error on more than one line (with its detail, hint, context or position),
// as only a first line ending with its location is decoded.
func (e *Error) Error() string {
	if e.Populate {
		return ""
	}
	var pe *pgconn.PgError
	if errors.As(e.Err, &pe) {
		multi := pe.Detail != "" || pe.Hint != "" || pe.Where != "" || pe.Position != 0 || pe.InternalPosition != 0
		if multi && !e.Full {
			return ""
		}
		// The message of libpq
		msg := pe.Severity + ":  " + pe.Message
		if pe.Detail != "" {
			msg += "\nDETAIL:  " + pe.Detail
		}
		if pe.Hint != "" {
			msg += "\nHINT:  " + pe.Hint
		}
		if pe.Where != "" {
			msg += "\nCONTEXT:  " + pe.Where
		}
		return fmt.Sprintf(`DBI Exception: DBD::Pg::st execute failed: %s [for Statement "%s"]`, msg, e.Query)
	}
	return e.Err.Error()
}
func (e *Error) Unwrap() error { return e.Err }

// Open opens a database. driver is "sqlite" or "postgres".
func Open(driver, dsn string) (*DB, error) {
	name := driver
	if driver == "sqlite" {
		name = sqliteDriver
	}
	raw, err := sql.Open(name, dsn)
	if err != nil {
		return nil, err
	}
	raw.SetMaxOpenConns(1)
	conn, err := raw.Conn(context.Background())
	if err != nil {
		raw.Close()
		return nil, err
	}
	d := &DB{raw: raw, conn: conn}
	if driver == "postgres" || driver == "pgx" {
		d.Dialect = Postgres
		return d, nil
	}
	// Equivalent of on_connect_call => 'use_foreign_keys'
	if _, err := conn.ExecContext(context.Background(), "PRAGMA foreign_keys = ON"); err != nil {
		d.Close()
		return nil, err
	}
	if err := d.addSQLiteIndexes(); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

// sqliteRecordIndex finds the later versions of a record (the record_later
// join of every search) by record, approval and time. Without it SQLite
// finds them through record_idx_approval, which matches almost every row,
// so that searches take time quadratic in the number of records (1 second
// to count 1000 records, 2 milliseconds with it). PostgreSQL chooses a hash
// join from its statistics, and needs no such index.
const sqliteRecordIndex = `CREATE INDEX IF NOT EXISTS "record_idx_current_id_approval_created" ON "record" ("current_id", "approval", "created", "id")`

// addSQLiteIndexes adds the indexes that a database deployed before they
// were added to the schema lacks (SQLite databases are not migrated).
func (d *DB) addSQLiteIndexes() error {
	var n int
	if err := d.conn.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'record'`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return nil // not deployed yet: Deploy creates it
	}
	_, err := d.conn.ExecContext(context.Background(), sqliteRecordIndex)
	return err
}

// OpenMemory opens a new in-memory SQLite database.
func OpenMemory() (*DB, error) {
	return Open("sqlite", ":memory:")
}

// Close closes the session.
func (d *DB) Close() error {
	d.conn.Close()
	return d.raw.Close()
}

// Deploy creates all GADS tables (as DBIx::Class deploy does).
func (d *DB) Deploy() error {
	schema := sqliteSchema
	if d.Dialect == Postgres {
		schema = postgresSchema
	}
	for _, stmt := range strings.Split(schema, ";\n") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := d.conn.ExecContext(context.Background(), stmt); err != nil {
			return fmt.Errorf("deploy: %w (%s)", err, stmt)
		}
	}
	return nil
}

// Q quotes an identifier.
func Q(name string) string {
	if i := strings.IndexByte(name, '.'); i >= 0 {
		return `"` + name[:i] + `"."` + name[i+1:] + `"`
	}
	return `"` + name + `"`
}

func (d *DB) trace(q string, args []any) {
	if d.Trace != nil {
		d.Trace(q, args)
	}
}

// sqlTrace is whether queries are printed to standard error
// (GADS_SQL_TRACE, as DBIC_TRACE for the Perl application)
var sqlTrace = os.Getenv("GADS_SQL_TRACE") != ""

func (d *DB) rebind(q string) string {
	if sqlTrace {
		fmt.Fprintln(os.Stderr, q)
	}
	if d.Dialect != Postgres {
		return q
	}
	var sb strings.Builder
	n := 0
	inStr := false
	for i := 0; i < len(q); i++ {
		c := q[i]
		if c == '\'' {
			inStr = !inStr
		}
		if c == '?' && !inStr {
			n++
			sb.WriteString(fmt.Sprintf("$%d", n))
			continue
		}
		sb.WriteByte(c)
	}
	return sb.String()
}

func convertArgs(args []any) []any {
	out := make([]any, len(args))
	for i, a := range args {
		switch v := a.(type) {
		case time.Time:
			out[i] = FormatDateTime(v)
		case Date:
			out[i] = FormatDate(time.Time(v))
		case *int64:
			if v == nil {
				out[i] = nil
			} else {
				out[i] = *v
			}
		case bool:
			if v {
				out[i] = 1
			} else {
				out[i] = 0
			}
		default:
			out[i] = a
		}
	}
	return out
}

// Exec runs a statement, panicking with *Error on failure.
func (d *DB) Exec(q string, args ...any) sql.Result {
	r, err := d.TryExec(q, args...)
	if err != nil {
		panic(err)
	}
	return r
}

// TryExec runs a statement returning any error.
func (d *DB) TryExec(q string, args ...any) (sql.Result, error) {
	d.checkWrite(q)
	args = convertArgs(args)
	d.trace(q, args)
	r, err := d.conn.ExecContext(context.Background(), d.rebind(q), args...)
	if err != nil {
		return nil, &Error{Query: q, Err: err, Full: d.FullErrors}
	}
	return r, nil
}

// Row is a result row keyed by column name.
type Row map[string]any

// Query runs a query and returns all rows.
func (d *DB) Query(q string, args ...any) []Row {
	rows, err := d.TryQuery(q, args...)
	if err != nil {
		panic(err)
	}
	return rows
}

// TryQuery runs a query returning rows or error.
func (d *DB) TryQuery(q string, args ...any) ([]Row, error) {
	d.checkWrite(q)
	args = convertArgs(args)
	d.trace(q, args)
	rs, err := d.conn.QueryContext(context.Background(), d.rebind(q), args...)
	if err != nil {
		return nil, &Error{Query: q, Err: err, Full: d.FullErrors}
	}
	defer rs.Close()
	cols, err := rs.Columns()
	if err != nil {
		return nil, &Error{Query: q, Err: err, Full: d.FullErrors}
	}
	// PostgreSQL dates and times are given as text, as DBD::Pg does (and
	// as SQLite stores them)
	var pgTypes []string
	if d.Dialect == Postgres {
		if cts, err := rs.ColumnTypes(); err == nil {
			for _, ct := range cts {
				pgTypes = append(pgTypes, ct.DatabaseTypeName())
			}
		}
	}
	var out []Row
	for rs.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rs.Scan(ptrs...); err != nil {
			return nil, &Error{Query: q, Err: err, Full: d.FullErrors}
		}
		row := make(Row, len(cols))
		for i, c := range cols {
			v := vals[i]
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			if t, ok := v.(time.Time); ok && i < len(pgTypes) {
				v = pgTimeText(t, pgTypes[i])
			}
			row[c] = v
		}
		out = append(out, row)
	}
	if err := rs.Err(); err != nil {
		return nil, &Error{Query: q, Err: err, Full: d.FullErrors}
	}
	return out, nil
}

// pgTimeText is the text of a PostgreSQL date or timestamp value
func pgTimeText(t time.Time, typ string) any {
	switch typ {
	case "DATE":
		return t.Format("2006-01-02")
	case "TIMESTAMP":
		if t.Nanosecond() != 0 {
			return strings.TrimRight(t.Format("2006-01-02 15:04:05.000000"), "0")
		}
		return t.Format("2006-01-02 15:04:05")
	}
	return t
}

// QueryRow returns the first row or nil.
func (d *DB) QueryRow(q string, args ...any) Row {
	rows := d.Query(q, args...)
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}

// QueryCol returns the first column of all rows.
func (d *DB) QueryCol(q string, args ...any) []any {
	rows, err := d.TryQueryCol(q, args...)
	if err != nil {
		panic(err)
	}
	return rows
}

// TryQueryCol is QueryCol returning an error.
func (d *DB) TryQueryCol(q string, args ...any) ([]any, error) {
	d.checkWrite(q)
	args = convertArgs(args)
	d.trace(q, args)
	rs, err := d.conn.QueryContext(context.Background(), d.rebind(q), args...)
	if err != nil {
		return nil, &Error{Query: q, Err: err, Full: d.FullErrors}
	}
	defer rs.Close()
	var out []any
	for rs.Next() {
		var v any
		if err := rs.Scan(&v); err != nil {
			return nil, &Error{Query: q, Err: err, Full: d.FullErrors}
		}
		if b, ok := v.([]byte); ok {
			v = string(b)
		}
		out = append(out, v)
	}
	if err := rs.Err(); err != nil {
		return nil, &Error{Query: q, Err: err, Full: d.FullErrors}
	}
	return out, nil
}

// QueryInt64s returns the first column as int64 values (NULLs skipped).
func (d *DB) QueryInt64s(q string, args ...any) []int64 {
	var out []int64
	for _, v := range d.QueryCol(q, args...) {
		if i, ok := AsInt64(v); ok {
			out = append(out, i)
		}
	}
	return out
}

// QueryInt64 returns a single integer value (0, false if NULL/no row).
func (d *DB) QueryInt64(q string, args ...any) (int64, bool) {
	vals := d.QueryCol(q, args...)
	if len(vals) == 0 {
		return 0, false
	}
	return AsInt64(vals[0])
}

// Insert inserts a row and returns its id. Columns are written in the
// order given by cols.
func (d *DB) Insert(table string, cols []string, vals ...any) int64 {
	id, err := d.TryInsert(table, cols, vals...)
	if err != nil {
		panic(err)
	}
	return id
}

// TryInsert inserts a row returning the new id or an error.
func (d *DB) TryInsert(table string, cols []string, vals ...any) (int64, error) {
	var sb strings.Builder
	sb.WriteString("INSERT INTO ")
	sb.WriteString(Q(table))
	if len(cols) == 0 {
		sb.WriteString(" DEFAULT VALUES")
	} else {
		sb.WriteString(" ( ")
		for i, c := range cols {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(Q(c))
		}
		sb.WriteString(") VALUES ( ")
		for i := range cols {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString("?")
		}
		sb.WriteString(" )")
	}
	// (Tables without an id column, as oauthtoken, return nothing)
	if d.Dialect == Postgres && ColumnType(table, "id") != "" {
		sb.WriteString(" RETURNING id")
		rows, err := d.TryQueryCol(sb.String(), vals...)
		if err != nil {
			return 0, err
		}
		if len(rows) > 0 {
			id, _ := AsInt64(rows[0])
			return id, nil
		}
		return 0, nil
	}
	r, err := d.TryExec(sb.String(), vals...)
	if err != nil {
		return 0, err
	}
	if d.Dialect == Postgres {
		return 0, nil
	}
	return r.LastInsertId()
}

// InsertMap inserts a row from a map, columns in sorted order (as
// DBIx::Class/SQL::Abstract does).
func (d *DB) InsertMap(table string, m map[string]any) int64 {
	cols, vals := sortedKV(m)
	return d.Insert(table, cols, vals...)
}

// TryInsertMap is InsertMap returning an error.
func (d *DB) TryInsertMap(table string, m map[string]any) (int64, error) {
	cols, vals := sortedKV(m)
	return d.TryInsert(table, cols, vals...)
}

// UpdateMap updates rows matching where (simple equality, AND).
func (d *DB) UpdateMap(table string, set map[string]any, where map[string]any) {
	scols, svals := sortedKV(set)
	if len(scols) == 0 {
		return
	}
	wcols, wvals := sortedKV(where)
	var sb strings.Builder
	sb.WriteString("UPDATE ")
	sb.WriteString(Q(table))
	sb.WriteString(" SET ")
	for i, c := range scols {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(Q(c) + " = ?")
	}
	args := append([]any{}, svals...)
	if len(wcols) > 0 {
		sb.WriteString(" WHERE ")
		for i, c := range wcols {
			if i > 0 {
				sb.WriteString(" AND ")
			}
			args = WhereTerm(&sb, c, wvals[i], args)
		}
	}
	d.Exec(sb.String(), args...)
}

// WhereTerm writes one equality condition. A list of values matches any of
// them (as DBIx::Class does for an arrayref), with nil matching NULL.
func WhereTerm(sb *strings.Builder, c string, v any, args []any) []any {
	switch x := v.(type) {
	case nil:
		sb.WriteString(Q(c) + " IS NULL")
	case []int64:
		if len(x) == 0 {
			sb.WriteString("0=1")
			break
		}
		sb.WriteString(Q(c) + " IN (" + Placeholders(len(x)) + ")")
		for _, id := range x {
			args = append(args, id)
		}
	case []any:
		if len(x) == 0 {
			sb.WriteString("0=1")
			break
		}
		var terms []string
		for _, e := range x {
			if e == nil {
				terms = append(terms, Q(c)+" IS NULL")
			} else {
				terms = append(terms, Q(c)+" = ?")
				args = append(args, e)
			}
		}
		sb.WriteString("( " + strings.Join(terms, " OR ") + " )")
	default:
		sb.WriteString(Q(c) + " = ?")
		args = append(args, v)
	}
	return args
}

// DeleteWhere deletes rows matching simple equality conditions.
func (d *DB) DeleteWhere(table string, where map[string]any) {
	wcols, wvals := sortedKV(where)
	var sb strings.Builder
	sb.WriteString("DELETE FROM ")
	sb.WriteString(Q(table))
	var args []any
	if len(wcols) > 0 {
		sb.WriteString(" WHERE ")
		for i, c := range wcols {
			if i > 0 {
				sb.WriteString(" AND ")
			}
			args = WhereTerm(&sb, c, wvals[i], args)
		}
	}
	d.Exec(sb.String(), args...)
}

func sortedKV(m map[string]any) ([]string, []any) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	vals := make([]any, len(keys))
	for i, k := range keys {
		vals[i] = m[k]
	}
	return keys, vals
}

// Placeholders returns "?, ?, ?" with n entries.
func Placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

// ---------------------------------------------------------------------
// Transactions (DBIx::Class txn_scope_guard semantics)
// ---------------------------------------------------------------------

// Begin starts a (possibly nested) transaction.
func (d *DB) Begin() {
	if d.depth == 0 {
		d.Exec("BEGIN")
	}
	d.depth++
}

// Commit commits the outermost transaction when the nesting reaches zero.
func (d *DB) Commit() {
	if d.depth == 0 {
		return
	}
	d.depth--
	if d.depth == 0 {
		d.Exec("COMMIT")
	}
}

// Rollback rolls back the transaction as a failed DBIC guard does: the
// outermost guard rolls back the whole transaction; a nested one only
// leaves its level (DBIC txn_rollback without auto_savepoint: the outer
// transaction continues)
func (d *DB) Rollback() {
	if d.depth == 0 {
		return
	}
	if d.depth > 1 {
		d.depth--
		return
	}
	d.depth = 0
	d.svp = 0
	d.TryExec("ROLLBACK")
}

// RollbackAll rolls back any transaction still open, whatever its depth
// (the state between requests of the web application)
func (d *DB) RollbackAll() {
	if d.depth > 0 {
		d.depth = 1
		d.Rollback()
	}
}

// InTxn reports whether a transaction is active.
func (d *DB) InTxn() bool { return d.depth > 0 }

// Txn runs f in a transaction guard: commit on success, rollback when f
// panics (the panic is re-raised).
func (d *DB) Txn(f func()) {
	d.Begin()
	ok := false
	defer func() {
		if !ok {
			d.Rollback()
		}
	}()
	f()
	d.Commit()
	ok = true
}

// SavepointBegin starts a savepoint (DBIC svp_begin), which as in DBIC
// needs a transaction.
func (d *DB) SavepointBegin() string {
	if d.depth == 0 {
		panic(errors.New("DBIx::Class::Storage::svp_begin(): You can't use savepoints outside a transaction"))
	}
	d.svp++
	name := fmt.Sprintf("savepoint_%d", d.svp-1)
	d.Exec("SAVEPOINT " + name)
	return name
}

// SavepointRollback rolls back to a savepoint (SavepointRelease then
// releases it).
func (d *DB) SavepointRollback(name string) {
	d.Exec("ROLLBACK TO SAVEPOINT " + name)
}

// SavepointRelease releases a savepoint.
func (d *DB) SavepointRelease(name string) {
	d.Exec("RELEASE SAVEPOINT " + name)
	d.svp--
}

// ---------------------------------------------------------------------
// Value helpers
// ---------------------------------------------------------------------

// Date marks a time value as a date-only value for binding.
type Date time.Time

// FormatDateTime formats as DateTime::Format::SQLite format_datetime.
func FormatDateTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

// FormatDate formats as DateTime::Format::SQLite format_date.
func FormatDate(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

// AsInt64 converts a database value to int64.
func AsInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case int:
		return int64(x), true
	case float64:
		return int64(x), true
	case string:
		var i int64
		_, err := fmt.Sscan(x, &i)
		return i, err == nil
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// AsString converts a database value to string (nil -> "", false).
func AsString(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "", false
	case string:
		return x, true
	case int64:
		return fmt.Sprint(x), true
	case float64:
		return fmt.Sprint(x), true
	case time.Time:
		return FormatDateTime(x), true
	}
	return fmt.Sprint(v), true
}

// AsTime converts a database value (string or time.Time) to UTC time.
func AsTime(v any) (time.Time, bool) {
	switch x := v.(type) {
	case time.Time:
		return time.Date(x.Year(), x.Month(), x.Day(), x.Hour(), x.Minute(), x.Second(), 0, time.UTC), true
	case string:
		return ParseDBDateTime(x)
	}
	return time.Time{}, false
}

// ParseDBDateTime parses values as DateTime::Format::SQLite does.
func ParseDBDateTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t, true
		}
	}
	// Non-padded forms
	var y, mo, da, h, mi, se int
	if n, _ := fmt.Sscanf(s, "%d-%d-%d %d:%d:%d", &y, &mo, &da, &h, &mi, &se); n == 6 {
		return time.Date(y, time.Month(mo), da, h, mi, se, 0, time.UTC), true
	}
	if n, _ := fmt.Sscanf(s, "%d-%d-%d", &y, &mo, &da); n == 3 {
		return time.Date(y, time.Month(mo), da, 0, 0, 0, 0, time.UTC), true
	}
	return time.Time{}, false
}

// IsUniqueViolation reports whether err is a unique constraint failure.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code == "23505"
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unique constraint failed") || strings.Contains(s, "duplicate")
}

var (
	colTypesOnce sync.Once
	colTypes     map[string]map[string]string
)

// ColumnType returns the declared type of a table column in the schema
// ("datetime", "date", "integer", ...), or "" if unknown.
func ColumnType(table, column string) string {
	colTypesOnce.Do(func() {
		colTypes = map[string]map[string]string{}
		tableRe := regexp.MustCompile(`(?m)^CREATE TABLE "([^"]+)" \(`)
		colRe := regexp.MustCompile(`^\s+"([^"]+)" ([a-zA-Z]+)`)
		var cur map[string]string
		for _, line := range strings.Split(sqliteSchema, "\n") {
			if m := tableRe.FindStringSubmatch(line); m != nil {
				cur = map[string]string{}
				colTypes[m[1]] = cur
				continue
			}
			if cur == nil {
				continue
			}
			if strings.HasPrefix(line, ")") {
				cur = nil
				continue
			}
			if m := colRe.FindStringSubmatch(line); m != nil {
				cur[m[1]] = strings.ToLower(m[2])
			}
		}
	})
	return colTypes[table][column]
}

// Populate inserts rows as DBIx::Class populate in void context: one
// prepared insert of the columns of the first row (without returning IDs),
// run for each row in turn and stopping at the first failure.
func (d *DB) Populate(table string, rows []map[string]any) {
	if len(rows) == 0 {
		return
	}
	cols, _ := sortedKV(rows[0])
	qs := make([]string, len(cols))
	qc := make([]string, len(cols))
	for i, c := range cols {
		qs[i] = "?"
		qc[i] = Q(c)
	}
	sql := "INSERT INTO " + Q(table) + " ( " + strings.Join(qc, ", ") + ") VALUES ( " + strings.Join(qs, ", ") + " )"
	for _, r := range rows {
		vals := make([]any, len(cols))
		for i, c := range cols {
			vals[i] = r[c]
		}
		if _, err := d.TryExec(sql, vals...); err != nil {
			err.(*Error).Populate = true
			panic(err)
		}
	}
}
