package cli

import (
	"bytes"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
)

func newEngine(t *testing.T, deploy bool) *gads.Engine {
	t.Helper()
	d, err := db.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	if deploy {
		if err := d.Deploy(); err != nil {
			t.Fatal(err)
		}
	}
	e, err := gads.NewEngine(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return e
}

func newTestEnv(e *gads.Engine, args []string, stdin string) (*Env, *bytes.Buffer, *bytes.Buffer) {
	var out, errb bytes.Buffer
	env := NewEnv(e, args, strings.NewReader(stdin), &out, &errb)
	env.Env = map[string]string{}
	return env, &out, &errb
}

func TestGetOptions(t *testing.T) {
	var site, name string
	var multi []string
	var flag, other bool
	env, _, errb := newTestEnv(nil, []string{"--site", "2", "-na=x", "--multi", "a", "--multi=b", "--FLAG", "file", "--", "--other"}, "")
	ok := env.GetOptions(
		Option{Name: "site-id", Value: true, Int: true, String: &site},
		Option{Name: "name", Value: true, String: &name},
		Option{Name: "multi", Value: true, Multi: true, Ints: &multi},
		Option{Name: "flag", Bool: &flag},
		Option{Name: "other", Bool: &other},
	)
	if !ok || site != "2" || name != "x" || !flag || other || !reflect.DeepEqual(multi, []string{"a", "b"}) {
		t.Errorf("got ok=%v site=%q name=%q flag=%v other=%v multi=%v (%s)", ok, site, name, flag, other, multi, errb)
	}
	if !reflect.DeepEqual(env.Args, []string{"file", "--other"}) {
		t.Errorf("remaining arguments %v", env.Args)
	}

	// The errors of Getopt::Long
	env, _, errb = newTestEnv(nil, []string{"--nope", "--id", "x", "--s"}, "")
	var id, s1 string
	ok = env.GetOptions(
		Option{Name: "id", Value: true, Int: true, String: &id},
		Option{Name: "site", Value: true, String: &s1},
		Option{Name: "size", Value: true, String: &s1},
	)
	want := "Unknown option: nope\nValue \"x\" invalid for option id (integer number expected)\nOption s is ambiguous (site, size)\n"
	if ok || errb.String() != want {
		t.Errorf("errors %q, want %q", errb.String(), want)
	}
	env, _, errb = newTestEnv(nil, []string{"--id"}, "")
	if env.GetOptions(Option{Name: "id", Value: true, String: &id}) || errb.String() != "Option id requires an argument\n" {
		t.Errorf("missing argument: %q", errb.String())
	}
}

func TestRun(t *testing.T) {
	Commands["zz-exit.pl"] = func(env *Env) { env.Say("out"); env.Exit(3) }
	Commands["zz-fail.pl"] = func(env *Env) { env.E.Notice("Note %d", 1); gads.Fail("Bad %s", "thing") }
	Commands["zz-die.pl"] = func(env *Env) { Die("Died here") }
	Commands["zz-ok.pl"] = func(env *Env) { env.E.Warn("careful") }
	defer func() {
		for _, n := range []string{"zz-exit.pl", "zz-fail.pl", "zz-die.pl", "zz-ok.pl"} {
			delete(Commands, n)
		}
	}()
	e := newEngine(t, false)
	cases := []struct {
		name, stdout, stderr string
		res                  Result
	}{
		{"zz-exit.pl", "out\n", "", Result{Exit: 3}},
		{"zz-fail.pl", "", "notice: Note 1\nerror: Bad thing\n", Result{Exit: 1}},
		{"zz-die.pl", "", "", Result{Exit: 255, Died: "Died here"}},
		{"zz-ok.pl", "", "warning: careful\n", Result{}},
		{"nonesuch.pl", "", "", Result{Exit: 255, Died: "Unknown script nonesuch.pl"}},
	}
	for _, c := range cases {
		env, out, errb := newTestEnv(e, nil, "")
		res := Run(c.name, env)
		if res != c.res || out.String() != c.stdout || errb.String() != c.stderr {
			t.Errorf("%s: %+v %q %q", c.name, res, out.String(), errb.String())
		}
	}
	if e.OnReport != nil {
		t.Errorf("OnReport not restored")
	}
}

func TestCSVLine(t *testing.T) {
	got := csvLine([]string{"plain", "with space", `q"uote`, "a,b", "", "line\nbreak"})
	want := "plain,\"with space\",\"q\"\"uote\",\"a,b\",,\"line\nbreak\"\n"
	if got != want {
		t.Errorf("csvLine = %q, want %q", got, want)
	}
}

func TestScheduleAll(t *testing.T) {
	got := scheduleAll(map[string][]string{"3": {"10"}, "10": {}, "2": {"3"}, "1": {}})
	want := []string{"1", "10", "3", "2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scheduleAll = %v, want %v", got, want)
	}
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("no error for a circular dependency")
		}
	}()
	scheduleAll(map[string][]string{"1": {"2"}, "2": {"1"}})
}

func TestColumnClass(t *testing.T) {
	for in, want := range map[string]string{"string": "GADS::Column::String", "createddate": "GADS::Column::Createddate", "my_type": "GADS::Column::MyType"} {
		if got := columnClass(in); got != want {
			t.Errorf("columnClass(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPerlValues(t *testing.T) {
	n := int64(5)
	var none *int64
	if pv(nil) != "undef" || pv(none) != "undef" || pv(&n) != "5" || pv(true) != "1" || pv(1.5) != "1.5" {
		t.Errorf("pv")
	}
	if pv(time.Date(2014, 1, 2, 3, 4, 5, 0, time.UTC)) != "2014-01-02T03:04:05" {
		t.Errorf("pv of a time")
	}
	if ps(nil) != "" || ps(none) != "" || ps("x") != "x" {
		t.Errorf("ps")
	}
	for in, want := range map[string]float64{"12abc": 12, "": 0, " 3.5 ": 3.5, "x": 0, "1e2": 100, "-.5": -0.5} {
		if got := pn(in); got != want {
			t.Errorf("pn(%q) = %v, want %v", in, got, want)
		}
	}
	if p := jint(int64(4)); p == nil || *p != 4 {
		t.Errorf("jint of an integer")
	}
	if jint("") != nil || jint(nil) != nil {
		t.Errorf("jint of nothing")
	}
}

func TestOnboardHelpers(t *testing.T) {
	want := "$VAR1 = {\n          'errors' => 3,\n          'in' => 4,\n          'written' => 1\n        };\n"
	if got := dumperCount(4, 1, 3); got != want {
		t.Errorf("dumperCount = %q", got)
	}
	if got := hTrim(" \t a b \t"); got != "a b" {
		t.Errorf("hTrim = %q", got)
	}
	if tm, ok := parseYMD("2013-2-28"); !ok || tm.Format("2006-01-02") != "2013-02-28" {
		t.Errorf("parseYMD of a date")
	}
	for _, bad := range []string{"2013-02-30", "bad", "2013-02-28x"} {
		if _, ok := parseYMD(bad); ok {
			t.Errorf("parseYMD(%q) succeeded", bad)
		}
	}
	err := gads.Try(func() { gads.Fail("Record ID %d not found", 99) })
	if got := tryStopped(err); got != "try-block stopped with ERROR: error: Record ID 99 not found\n" {
		t.Errorf("tryStopped = %q", got)
	}
	if got := errMessage(errors.New("plain")); got != "plain" {
		t.Errorf("errMessage = %q", got)
	}
}

func TestSeedDatabase(t *testing.T) {
	e := newEngine(t, false)
	env, out, errb := newTestEnv(e, []string{"--site", "gads.example.com"}, "Sheet\nadmin@example.com\n")
	env.Env["TEST"] = "1"
	if res := Run("seed-database.pl", env); res != (Result{}) {
		t.Fatalf("seed-database: %+v %s", res, errb)
	}
	want := "Please enter the name of the first datasheet\nPlease enter the email address of the first user\nInstalling schema and fixtures if needed...\n" +
		"Creating site \"gads.example.com\"...\nCreating initial username \"admin@example.com\"...\nAdding all permissions to initial username...\n" +
		"Creating basic user \"basic@example.com\" with no permissions...\nCreating group for basic user\nAdding basic user to basic group\nCreating initial datasheet...\n"
	if out.String() != want {
		t.Errorf("output %q", out.String())
	}
	d := e.DB
	check := func(q string, want int64) {
		if n, _ := d.QueryInt64(q); n != want {
			t.Errorf("%s = %d, want %d", q, n, want)
		}
	}
	check(`SELECT COUNT(*) FROM "permission"`, 3)
	check(`SELECT COUNT(*) FROM "user_permission" WHERE "user_id" = 1`, 3)
	check(`SELECT COUNT(*) FROM "user" WHERE "site_id" = 1`, 2)
	check(`SELECT COUNT(*) FROM "user_group"`, 1)
	check(`SELECT COUNT(*) FROM "instance" WHERE "name" = 'Sheet' AND "site_id" = 1`, 1)

	// Again on the database now deployed: a second site
	env, _, errb = newTestEnv(e, []string{"--site", "two", "--instance_name", "S2", "--initial_username", "b@example.com"}, "")
	if res := Run("seed-database.pl", env); res != (Result{}) {
		t.Fatalf("second seed-database: %+v %s", res, errb)
	}
	check(`SELECT COUNT(*) FROM "permission"`, 3)
	check(`SELECT COUNT(*) FROM "site"`, 2)
}

// TestDateFormat runs date-format.pl with IGNORE_PERMISSIONS set, which
// the Perl script does not set (so it finds no tables there, as the
// conformance scenario shows): the dates of the filters are rewritten
func TestDateFormat(t *testing.T) {
	e := newEngine(t, true)
	d := e.DB
	site := d.InsertMap("site", map[string]any{"host": "localhost"})
	inst := d.InsertMap("instance", map[string]any{"name": "T", "site_id": site})
	col := d.InsertMap("layout", map[string]any{"name": "d", "type": "date", "instance_id": inst})
	view := d.InsertMap("view", map[string]any{"name": "v", "instance_id": inst, "global": 1,
		"filter": `{"rules":[{"id":` + strconv.FormatInt(col, 10) + `,"type":"date","operator":"equal","value":"2014-10-10"}]}`})
	e.IgnorePermissions = true
	env, _, errb := newTestEnv(e, []string{"--to", "dd-MM-yyyy"}, "")
	if res := Run("date-format.pl", env); res != (Result{}) {
		t.Fatalf("date-format: %+v %s", res, errb)
	}
	f := d.QueryRow(`SELECT "filter" FROM "view" WHERE "id" = ?`, view)
	if s := str(f["filter"]); !strings.Contains(s, `"10-10-2014"`) {
		t.Errorf("filter %s", s)
	}
	env, _, errb = newTestEnv(e, nil, "")
	if res := Run("date-format.pl", env); res.Exit != 1 || errb.String() != "error: Please provide new date format with --to\n" {
		t.Errorf("without --to: %+v %q", res, errb)
	}
}
