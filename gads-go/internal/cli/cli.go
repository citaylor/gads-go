// Package cli is the command line scripts of GADS (its bin directory),
// each run against an engine with its arguments and standard streams.
package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
)

// Env is the environment of a script: the engine of its database, its
// arguments and its standard streams
type Env struct {
	E      *gads.Engine
	Args   []string
	Stdin  *bufio.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Dir is the current directory, in which relative paths are (the
	// process directory if empty)
	Dir string
	// Env is the environment of the process (os.Getenv if nil)
	Env map[string]string
}

// Getenv is a variable of the environment of the script
func (env *Env) Getenv(name string) string {
	if env.Env != nil {
		return env.Env[name]
	}
	return os.Getenv(name)
}

// Path is a path of the script relative to its current directory
func (env *Env) Path(p string) string {
	if env.Dir == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(env.Dir, p)
}

// NewEnv makes the environment of a script
func NewEnv(e *gads.Engine, args []string, stdin io.Reader, stdout, stderr io.Writer) *Env {
	return &Env{E: e, Args: args, Stdin: bufio.NewReader(stdin), Stdout: stdout, Stderr: stderr}
}

// exitCode ends a script with an exit status (Perl exit)
type exitCode int

// Exit ends the script with a status
func (env *Env) Exit(code int) { panic(exitCode(code)) }

// Say is Perl say: a line to the standard output
func (env *Env) Say(format string, a ...any) { fmt.Fprintf(env.Stdout, format+"\n", a...) }

// SayErr is Perl say STDERR
func (env *Env) SayErr(format string, a ...any) { fmt.Fprintf(env.Stderr, format+"\n", a...) }

// ReadLine is <STDIN>: a line of the standard input (with its newline),
// or "" at its end
func (env *Env) ReadLine() string {
	line, _ := env.Stdin.ReadString('\n')
	return line
}

// Command is a script
type Command func(env *Env)

// Commands are the scripts by the name of the Perl script
var Commands = map[string]Command{}

// Names are the names of the scripts
func Names() []string {
	var names []string
	for n := range Commands {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Result is the outcome of a script: its exit status, and the message of
// an error that ended it (Perl die), if any
type Result struct {
	Exit int
	Died string
}

// Run runs a script. An error raised by it (gads.Fail or Panic, or a die
// of the script) ends it with the status of a Perl die (255).
func Run(name string, env *Env) (res Result) {
	cmd, ok := Commands[name]
	if !ok {
		return Result{Exit: 255, Died: "Unknown script " + name}
	}
	// Reports are written as the default dispatcher of Log::Report does:
	// the reason and the message
	oldReport := env.E.OnReport
	env.E.OnReport = func(r gads.Report) {
		fmt.Fprintf(env.Stderr, "%s: %s\n", strings.ToLower(string(r.Level)), r.Msg)
	}
	oldWarn := env.E.PerlWarn
	env.E.PerlWarn = func(msg string) { fmt.Fprint(env.Stderr, msg) }
	defer func() { env.E.OnReport, env.E.PerlWarn = oldReport, oldWarn }()
	defer func() {
		if r := recover(); r != nil {
			switch x := r.(type) {
			case exitCode:
				res = Result{Exit: int(x)}
			case *gads.Error:
				// A fatal report is written and ends the script with 1
				level := x.Level
				if level == "" {
					level = gads.LevelError
				}
				fmt.Fprintf(env.Stderr, "%s: %s\n", strings.ToLower(string(level)), x.Msg)
				res = Result{Exit: 1}
			case *db.Error:
				// A DBIC exception is panicked by the exception_action
				// GADS sets on the schema
				fmt.Fprintf(env.Stderr, "panic: %s\n", x.Error())
				res = Result{Exit: 1}
			case dieError:
				res = Result{Exit: 255, Died: string(x)}
			default:
				res = Result{Exit: 255, Died: fmt.Sprint(x)}
			}
		}
	}()
	cmd(env)
	return Result{}
}

// dieError is a Perl die of a script (not a Log::Report error)
type dieError string

// Die is Perl die
func Die(format string, a ...any) { panic(dieError(fmt.Sprintf(format, a...))) }

// ---------------------------------------------------------------------
// Getopt::Long
// ---------------------------------------------------------------------

// Option is an option of GetOptions: its name, whether it takes a value
// ("=s", "=i") and where the value goes
type Option struct {
	Name   string
	Value  bool
	Int    bool
	Multi  bool
	String *string
	Ints   *[]string
	Bool   *bool
}

// GetOptions is Getopt::Long GetOptions: the options are removed from the
// arguments (leaving the others), with a warning on the standard error for
// each that is wrong. It returns false if any is.
func (env *Env) GetOptions(opts ...Option) bool {
	ok := true
	var rest []string
	args := env.Args
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			rest = append(rest, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		value, hasValue := "", false
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			name, value, hasValue = name[:eq], name[eq+1:], true
		}
		opt, found := findOption(opts, name)
		if amb := ambiguous(opts, name); !found && len(amb) > 1 {
			fmt.Fprintf(env.Stderr, "Option %s is ambiguous (%s)\n", name, strings.Join(amb, ", "))
			ok = false
			continue
		}
		if !found {
			fmt.Fprintf(env.Stderr, "Unknown option: %s\n", strings.ToLower(name))
			ok = false
			continue
		}
		if !opt.Value {
			if opt.Bool != nil {
				*opt.Bool = true
			}
			continue
		}
		if !hasValue {
			if i+1 >= len(args) {
				fmt.Fprintf(env.Stderr, "Option %s requires an argument\n", name)
				ok = false
				continue
			}
			i++
			value = args[i]
		}
		if opt.Int && !isInt(value) {
			fmt.Fprintf(env.Stderr, "Value \"%s\" invalid for option %s (integer number expected)\n", value, name)
			ok = false
			continue
		}
		if opt.Multi && opt.Ints != nil {
			*opt.Ints = append(*opt.Ints, value)
		} else if opt.String != nil {
			*opt.String = value
		}
	}
	env.Args = rest
	return ok
}

// findOption finds an option by its name or a unique abbreviation of it
// (ignoring case, as Getopt::Long by default)
func findOption(opts []Option, name string) (Option, bool) {
	lname := strings.ToLower(name)
	var matches []Option
	for _, o := range opts {
		if strings.ToLower(o.Name) == lname {
			return o, true
		}
		if strings.HasPrefix(strings.ToLower(o.Name), lname) {
			matches = append(matches, o)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	return Option{}, false
}

// ambiguous are the names of the options that an abbreviation matches,
// sorted
func ambiguous(opts []Option, name string) []string {
	var names []string
	for _, o := range opts {
		if strings.HasPrefix(strings.ToLower(o.Name), strings.ToLower(name)) {
			names = append(names, o.Name)
		}
	}
	sort.Strings(names)
	return names
}

func isInt(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == '-' || s[0] == '+' {
		s = s[1:]
	}
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
