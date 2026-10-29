package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// logger is the log of gads serve (Go only): its level is the Dancer2 log
// setting (core and debug: debug, info, warning, error; default info) and
// its format gads: log_format (text, the default, or json)
func (c *appConfig) logger(w io.Writer) (*slog.Logger, error) {
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
		return nil, fmt.Errorf("Unknown log level %s", l)
	}
	opts := &slog.HandlerOptions{Level: level}
	switch f := asString(c.section("gads")["log_format"]); f {
	case "", "text":
		return slog.New(slog.NewTextHandler(w, opts)), nil
	case "json":
		return slog.New(slog.NewJSONHandler(w, opts)), nil
	default:
		return nil, fmt.Errorf("Unknown log_format %s", f)
	}
}

// healthcheck is gads healthcheck: exits 0 if the health check of a
// running server answers 200 (the HEALTHCHECK of the Docker image, which
// has no curl)
func healthcheck(args []string) int {
	fs := flag.NewFlagSet("healthcheck", flag.ExitOnError)
	url := fs.String("url", "http://127.0.0.1:3000/healthz", "health check URL")
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
