package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// loggers are the logs of gads serve (Go only), in the format of gads:
// log_format (text, the default, or json): that of errors and other
// messages, at the Dancer2 log level (core and debug: debug, info,
// warning, error; default info), and that of requests, unless gads:
// access_log: 0, whatever the level (GADS's production environment sets
// log: warning).
func (c *appConfig) loggers(w io.Writer) (logger, access *slog.Logger, err error) {
	var level slog.Level
	switch l := strings.ToLower(asString(c.raw["log"])); l {
	case "core", "debug":
		level = slog.LevelDebug
	case "", "info":
		level = slog.LevelInfo
	case "warning", "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		return nil, nil, fmt.Errorf("Unknown log level %s", l)
	}
	g := c.section("gads")
	var handler func(*slog.HandlerOptions) slog.Handler
	switch f := asString(g["log_format"]); f {
	case "", "text":
		handler = func(o *slog.HandlerOptions) slog.Handler { return slog.NewTextHandler(w, o) }
	case "json":
		handler = func(o *slog.HandlerOptions) slog.Handler { return slog.NewJSONHandler(w, o) }
	default:
		return nil, nil, fmt.Errorf("Unknown log_format %s", f)
	}
	logger = slog.New(handler(&slog.HandlerOptions{Level: level}))
	if v, ok := g["access_log"]; !ok || asBool(v) {
		access = slog.New(handler(&slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	return logger, access, nil
}

// listenAddress is the default address of gads serve: GADS_LISTEN, or :3000
func listenAddress() string {
	if a := os.Getenv("GADS_LISTEN"); a != "" {
		return a
	}
	return ":3000"
}

// healthURL is the health check URL of a server listening on addr (on this
// host when it listens on all addresses)
func healthURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://127.0.0.1:3000/healthz"
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz"
}

// healthcheck is gads healthcheck: exits 0 if the health check of a
// running server answers 200 (the HEALTHCHECK of the Docker image, which
// has no curl)
func healthcheck(args []string) int {
	fs := flag.NewFlagSet("healthcheck", flag.ExitOnError)
	url := fs.String("url", healthURL(listenAddress()), "health check URL (default from GADS_LISTEN)")
	timeout := fs.Duration("timeout", 15*time.Second, "time to wait for the answer")
	fs.Parse(args)
	client := &http.Client{Timeout: *timeout}
	res, err := client.Get(*url)
	if err != nil {
		return fail(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "health check: %s\n", res.Status)
		return 1
	}
	return 0
}
