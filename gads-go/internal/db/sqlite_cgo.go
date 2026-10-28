//go:build !modernc

package db

// The SQLite driver: the C SQLite library (as DBD::SQLite), through cgo.
// Build with -tags modernc for the pure Go translation instead, which
// needs no C compiler but is about 2.5 times slower on large queries.
import _ "github.com/mattn/go-sqlite3"

const sqliteDriver = "sqlite3"
