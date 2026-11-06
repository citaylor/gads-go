//go:build !modernc

package db

// The SQLite driver: the C SQLite library (as DBD::SQLite), through cgo.
// Build with -tags modernc for the pure Go translation instead, which
// needs no C compiler but is about 2.5 times slower on large queries.
import (
	"errors"

	"github.com/mattn/go-sqlite3"
)

const sqliteDriver = "sqlite3"

// sqlitePrepareError is whether SQLite failed to compile the statement
// (SQLITE_ERROR: an unknown column, a syntax error), which DBI reports from
// prepare_cached, rather than to run it
func sqlitePrepareError(err error) bool {
	var se sqlite3.Error
	return errors.As(err, &se) && se.Code == sqlite3.ErrError
}
