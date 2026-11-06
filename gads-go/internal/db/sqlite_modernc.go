//go:build modernc

package db

// The SQLite driver with -tags modernc: modernc.org/sqlite, SQLite
// translated to Go (no cgo), slower than the default C library.
import (
	"errors"

	"modernc.org/sqlite"
)

const sqliteDriver = "sqlite"

// sqlitePrepareError is whether SQLite failed to compile the statement
// (SQLITE_ERROR: an unknown column, a syntax error), which DBI reports from
// prepare_cached, rather than to run it
func sqlitePrepareError(err error) bool {
	var se *sqlite.Error
	return errors.As(err, &se) && se.Code() == 1
}
