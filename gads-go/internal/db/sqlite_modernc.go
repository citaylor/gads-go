//go:build modernc

package db

// The SQLite driver with -tags modernc: modernc.org/sqlite, SQLite
// translated to Go (no cgo), slower than the default C library.
import _ "modernc.org/sqlite"

const sqliteDriver = "sqlite"
