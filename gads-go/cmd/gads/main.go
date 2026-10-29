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
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
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
  healthcheck [-url URL]
                 check that a running server is healthy (exit status 0)
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
	case "healthcheck":
		os.Exit(healthcheck(args))
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

// mailDrainTimeout is how long the emails still queued at exit are waited
// for
const mailDrainTimeout = time.Minute

// open opens the database and engine of a configuration, with the mailer
// of its emails (to be closed at exit, delivering those still queued)
func open(dir string) (*appConfig, *gads.Engine, *mailer, error) {
	cfg, err := loadConfig(dir, environment())
	if err != nil {
		return nil, nil, nil, err
	}
	driver, dsn, err := cfg.database()
	if err != nil {
		return nil, nil, nil, err
	}
	mc, err := cfg.mailConfig()
	if err != nil {
		return nil, nil, nil, err
	}
	d, err := db.Open(driver, dsn)
	if err != nil {
		return nil, nil, nil, err
	}
	e, err := gads.NewEngine(d, cfg.engineConfig())
	if err != nil {
		d.Close()
		return nil, nil, nil, err
	}
	m := newMailer(mc, asString(cfg.section("gads")["email_from"]))
	e.Emailer = m.Send
	return cfg, e, m, nil
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, err)
	return 1
}

func serve(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	conf := fs.String("config", "", "configuration directory (with config.yml)")
	listen := fs.String("listen", listenAddress(), "address to listen on (default GADS_LISTEN, or :3000)")
	views := fs.String("views", "", "directory of the views (default <config>/views)")
	public := fs.String("public", "", "directory of the static files (default <config>/public)")
	fs.Parse(args)
	dir := configDir(*conf)
	cfg, e, m, err := open(dir)
	if err != nil {
		return fail(err)
	}
	defer m.Close(mailDrainTimeout)
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
	logger, access, err := cfg.loggers(os.Stdout)
	if err != nil {
		return fail(err)
	}
	// Other log output (log.Printf, mail failures) goes to the same log
	slog.SetDefault(logger)
	wcfg := cfg.webConfig()
	wcfg.Logger, wcfg.AccessLogger = logger, access
	srv := web.New(e, wcfg, *views, *public)
	logger.Info("GADS listening", "address", *listen)
	server := &http.Server{
		Addr:    *listen,
		Handler: srv,
		// Slow clients are dropped (large uploads still have ReadTimeout)
		ReadHeaderTimeout: 30 * time.Second,
		ReadTimeout:       10 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
	// SIGINT and SIGTERM stop accepting requests and let those in progress
	// finish
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- server.ListenAndServe() }()
	select {
	case err := <-errc:
		return fail(err)
	case <-ctx.Done():
	}
	logger.Info("GADS shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(sctx); err != nil {
		return fail(err)
	}
	return 0
}

func deploy(args []string) int {
	fs := flag.NewFlagSet("deploy", flag.ExitOnError)
	conf := fs.String("config", "", "configuration directory (with config.yml)")
	fs.Parse(args)
	_, e, m, err := open(configDir(*conf))
	if err != nil {
		return fail(err)
	}
	defer m.Close(mailDrainTimeout)
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
	_, e, m, err := open(configDir(""))
	if err != nil {
		return fail(err)
	}
	env := cli.NewEnv(e, args, os.Stdin, os.Stdout, os.Stderr)
	res := cli.Run(name, env)
	m.Close(mailDrainTimeout)
	if res.Died != "" {
		fmt.Fprintln(os.Stderr, res.Died)
	}
	return res.Exit
}
