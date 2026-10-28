// Command gads is the GADS application: its web server (serve), the
// creation of its database tables (deploy), and the scripts of its bin
// directory, each as a subcommand by its name (with or without .pl).
//
//	gads serve [-config DIR] [-listen :3000]
//	gads deploy [-config DIR]
//	gads export.pl --site-id 1
//	gads list
//
// The configuration is that of the Perl application: config.yml in the
// configuration directory (default the current directory), merged with
// environments/$DANCER_ENVIRONMENT.yml (default development).
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/ctrlo/gads-go/internal/cli"
	"github.com/ctrlo/gads-go/internal/db"
	"github.com/ctrlo/gads-go/internal/gads"
	"github.com/ctrlo/gads-go/internal/web"
)

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: gads <command> [options]

Commands:
  serve [-config DIR] [-listen ADDR] [-views DIR] [-public DIR]
                 run the web application
  deploy [-config DIR]
                 create the tables of the database
  list           list the scripts
  <script>       run a script of bin (as export.pl or export), with its
                 options; GADS_CONFIG is the configuration directory
`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "serve":
		os.Exit(serve(args))
	case "deploy":
		os.Exit(deploy(args))
	case "list":
		for _, n := range cli.Names() {
			fmt.Println(n)
		}
	case "-h", "-help", "--help", "help":
		usage()
	default:
		os.Exit(script(cmd, args))
	}
}

func environment() string {
	if e := os.Getenv("DANCER_ENVIRONMENT"); e != "" {
		return e
	}
	return "development"
}

func configDir(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if d := os.Getenv("GADS_CONFIG"); d != "" {
		return d
	}
	return "."
}

// open opens the database and engine of a configuration
func open(dir string) (*appConfig, *gads.Engine, error) {
	cfg, err := loadConfig(dir, environment())
	if err != nil {
		return nil, nil, err
	}
	driver, dsn, err := cfg.database()
	if err != nil {
		return nil, nil, err
	}
	d, err := db.Open(driver, dsn)
	if err != nil {
		return nil, nil, err
	}
	e, err := gads.NewEngine(d, cfg.engineConfig())
	if err != nil {
		d.Close()
		return nil, nil, err
	}
	mc, err := cfg.mailConfig()
	if err != nil {
		d.Close()
		return nil, nil, err
	}
	e.Emailer = newMailer(mc, asString(cfg.section("gads")["email_from"])).Send
	return cfg, e, nil
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, err)
	return 1
}

func serve(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	conf := fs.String("config", "", "configuration directory (with config.yml)")
	listen := fs.String("listen", ":3000", "address to listen on")
	views := fs.String("views", "", "directory of the views (default <config>/views)")
	public := fs.String("public", "", "directory of the static files (default <config>/public)")
	fs.Parse(args)
	dir := configDir(*conf)
	cfg, e, err := open(dir)
	if err != nil {
		return fail(err)
	}
	if *views == "" {
		*views = filepath.Join(dir, "views")
	}
	if *public == "" {
		*public = filepath.Join(dir, "public")
	}
	// As GADS on startup: the internal columns of all tables are created
	// if missing
	if err := gads.Try(func() {
		e.IgnorePermissions = true
		defer func() { e.IgnorePermissions = false }()
		for _, l := range e.NewInstances(nil).All() {
			l.CreateInternalColumns()
		}
	}); err != nil {
		return fail(err)
	}
	srv := web.New(e, cfg.webConfig(), *views, *public)
	fmt.Fprintf(os.Stderr, "GADS listening on %s\n", *listen)
	server := &http.Server{
		Addr:    *listen,
		Handler: srv,
		// Slow clients are dropped (large uploads still have ReadTimeout)
		ReadHeaderTimeout: 30 * time.Second,
		ReadTimeout:       10 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
	if err := server.ListenAndServe(); err != nil {
		return fail(err)
	}
	return 0
}

func deploy(args []string) int {
	fs := flag.NewFlagSet("deploy", flag.ExitOnError)
	conf := fs.String("config", "", "configuration directory (with config.yml)")
	fs.Parse(args)
	_, e, err := open(configDir(*conf))
	if err != nil {
		return fail(err)
	}
	if err := e.DB.Deploy(); err != nil {
		return fail(err)
	}
	return 0
}

func script(name string, args []string) int {
	if !strings.HasSuffix(name, ".pl") {
		name += ".pl"
	}
	if _, ok := cli.Commands[name]; !ok {
		fmt.Fprintf(os.Stderr, "Unknown command %s\n", strings.TrimSuffix(name, ".pl"))
		usage()
		return 2
	}
	_, e, err := open(configDir(""))
	if err != nil {
		return fail(err)
	}
	env := cli.NewEnv(e, args, os.Stdin, os.Stdout, os.Stderr)
	res := cli.Run(name, env)
	if res.Died != "" {
		fmt.Fprintln(os.Stderr, res.Died)
	}
	return res.Exit
}
