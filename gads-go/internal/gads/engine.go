// Package gads is a Go re-implementation of the GADS (Globally Accessible
// Data Store) engine by Ctrl O. It operates on the same database schema as
// the original Perl application and reproduces its behaviour, as verified by
// the language-neutral conformance suite in ../conformance.
//
// Error handling mirrors the original's Log::Report exception model: deep
// code raises errors with Fail()/Panic() and public entry points recover
// them with Try(), returning ordinary Go errors.
package gads

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/lua"
)

// Engine holds the state shared by all GADS objects (equivalent of the
// schema, config and Inline::Lua interpreter singletons in Perl).
type Engine struct {
	// PerlWarn receives the warnings that Perl gives on stderr (such as
	// of uninitialized values) where those are visible (the scripts)
	PerlWarn func(msg string)

	DB     *db.DB
	Config *Config
	Lua    *lua.Interpreter
	// Clock returns the current time. Tests freeze it (Test::MockTime).
	Clock func() time.Time
	// SiteID limits all queries to a site (GADS::Schema site_id)
	SiteID int64
	// Reports collects non-fatal messages (Log::Report non-fatal reports)
	Reports []Report
	// OnReport, if set, is given the non-fatal messages instead (as the
	// dispatchers of the command line scripts)
	OnReport func(Report)
	// EnumMapping, if set, is given the IDs of the enumvals written from
	// those of a source_id (the enum_mapping of bin/import.pl)
	EnumMapping map[int64]int64

	// Global flags equivalent to $GADS::Schema::IGNORE_PERMISSIONS and
	// $GADS::Schema::IGNORE_PERMISSIONS_SEARCH
	IgnorePermissions bool
	// PanicOnCode panics if code values are evaluated rather than loaded
	// (GADS_PANIC_ON_ENTERING_CODE)
	PanicOnCode             bool
	IgnorePermissionsSearch bool
	// AllowBlankCurval allows curval fields without any fields
	// (GADS_ALLOW_BLANK_CURVAL)
	AllowBlankCurval bool
	// PanicOnCurvalBuildValues panics if all of a curval's values are built
	// (PANIC_ON_CURVAL_BUILD_VALUES)
	PanicOnCurvalBuildValues bool

	// Alert sender, run after records and code values change (ProcessAlerts
	// by default; nil disables alerts)
	AlertSender func(e *Engine, layout *Layout, currentIDs []int64, columns []int64, isNew bool)
	// NoFork processes alerts as GADS does with GADS_NO_FORK set (as in its
	// tests): inline, after which Record write returns before clearing its
	// new entry flags. Without it, alerts are still processed inline (Perl
	// forks a process for them) but the record write then completes.
	NoFork bool
	// Email sender (optional): receives the emails of SendEmail
	Emailer func(Email)

	// TokenGenerator replaces the random token generator (tests)
	TokenGenerator func(n int) string
	// PasswordGenerator replaces the random password generator (tests)
	PasswordGenerator func() string
	// SAMLIDGenerator replaces the random IDs of SAML requests (tests)
	SAMLIDGenerator func() string
}

// Config is the GADS application configuration (GADS::Config).
type Config struct {
	DateFormat    string
	URL           string
	LoginInstance int64
	Header        string
	// Uploads is the directory of uploaded files (gads.uploads, default
	// /var/lib/GADS/uploads)
	Uploads string
	// Yubi is the Yubikey API configuration (gads.yubi: id, key, and url
	// for an alternative verification server)
	Yubi map[string]string
	// SMS is the SMS gateway configuration (gads.sms: username, password,
	// from, and url for an alternative gateway)
	SMS map[string]string
	// Hardened fixes the security findings of GADS
	// (GADS-security-report-verified.md), each a deliberate deviation from
	// Perl. It is off by default, so that the conformance suite compares
	// Go with Perl as it is, and on in the application (gads: hardened).
	Hardened bool
	// PerlPasswordHashes keeps writing the SSHA512 password hashes of Perl
	// in hardened mode, for a database that Perl GADS also uses
	// (gads: perl_password_hashes)
	PerlPasswordHashes bool
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{DateFormat: "yyyy-MM-dd", URL: "http://localhost", LoginInstance: 1}
}

// NewEngine creates an engine for an open database.
func NewEngine(d *db.DB, cfg *Config) (*Engine, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	l, err := lua.New()
	if err != nil {
		return nil, err
	}
	return &Engine{
		DB:     d,
		Config: cfg,
		Lua:    l,
		Clock:  func() time.Time { return time.Now().UTC() },

		AlertSender: ProcessAlerts,
	}, nil
}

// Now returns the current (possibly frozen) time in UTC, truncated to
// seconds as DateTime->now is.
func (e *Engine) Now() time.Time {
	return e.Clock().UTC().Truncate(time.Second)
}

// Close releases engine resources.
func (e *Engine) Close() {
	if e.Lua != nil {
		e.Lua.Close()
	}
}

// ---------------------------------------------------------------------
// Errors (Log::Report equivalents)
// ---------------------------------------------------------------------

// Level of a report
type Level string

const (
	LevelError   Level = "ERROR"
	LevelMistake Level = "MISTAKE"
	LevelWarning Level = "WARNING"
	LevelNotice  Level = "NOTICE"
	LevelPanic   Level = "PANIC"
)

// Error is a user-facing fatal error (Log::Report "error").
type Error struct {
	Msg   string
	Level Level
}

func (e *Error) Error() string { return e.Msg }

// Report is a non-fatal message.
type Report struct {
	Level Level
	Msg   string
}

// Fail raises a user error (Log::Report error/__x).
func Fail(format string, args ...any) {
	panic(&Error{Msg: sprintf(format, args...), Level: LevelError})
}

// Panic raises an internal error (Log::Report panic).
func Panic(format string, args ...any) {
	panic(&Error{Msg: sprintf(format, args...), Level: LevelPanic})
}

func sprintf(format string, args ...any) string {
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}

func (e *Engine) addReport(r Report) {
	if e.OnReport != nil {
		e.OnReport(r)
		return
	}
	e.Reports = append(e.Reports, r)
}

// perlWarn gives a warning of Perl (at a location of a module)
func (e *Engine) perlWarn(msg, module string) {
	if e.PerlWarn != nil {
		e.PerlWarn(msg + " at lib/GADS/" + module + " line 1.\n")
	}
}

// Warn records a non-fatal warning.
func (e *Engine) Warn(format string, args ...any) {
	e.addReport(Report{Level: LevelWarning, Msg: sprintf(format, args...)})
}

// Mistake records a non-fatal mistake report.
func (e *Engine) Mistake(format string, args ...any) {
	e.addReport(Report{Level: LevelMistake, Msg: sprintf(format, args...)})
}

// ReportError records a non-fatal error (report { is_fatal => 0 }, ERROR).
func (e *Engine) ReportError(format string, args ...any) {
	e.addReport(Report{Level: LevelError, Msg: sprintf(format, args...)})
}

// Notice records a notice.
func (e *Engine) Notice(format string, args ...any) {
	e.addReport(Report{Level: LevelNotice, Msg: sprintf(format, args...)})
}

// Try runs f, recovering its panic as an error: GADS and database errors as
// they are, any other (a runtime error) as an internal error (LevelPanic).
func Try(f func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			switch x := r.(type) {
			case *Error:
				err = x
			case *db.Error:
				err = x
			case error:
				// GADS_STACK prints the stack of runtime errors (debugging)
				if os.Getenv("GADS_STACK") != "" {
					fmt.Fprintf(os.Stderr, "%v\n%s\n", x, debug.Stack())
				}
				err = &Error{Msg: x.Error(), Level: LevelPanic}
			default:
				err = &Error{Msg: fmt.Sprint(x), Level: LevelPanic}
			}
		}
	}()
	f()
	return nil
}

// TxnTry is Try of f in a transaction, rolled back on failure.
func (e *Engine) TxnTry(f func()) error {
	return Try(func() { e.DB.Txn(f) })
}

// q is a helper for quoted identifiers
func q(s string) string { return db.Q(s) }

// ucfirst as Perl
func ucfirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
