// Command goflexlmweb serves a local license purchasing and usage dashboard.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	store "github.com/danofsteel32/goflexlm/sqlite"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("goflexlmweb", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dbPath := flags.String("db", "", "existing SQLite database")
	pool := flags.String("pool", "", "default license pool")
	listen := flags.String("listen", "127.0.0.1:8080", "loopback address and port")
	if err := flags.Parse(args); err != nil || strings.TrimSpace(*dbPath) == "" || strings.TrimSpace(*pool) == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: goflexlmweb --db DB --pool POOL [--listen 127.0.0.1:8080]")
		return 2
	}
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		fmt.Fprintln(stderr, "goflexlmweb: --listen must use a loopback IP address")
		return 2
	}
	info, err := os.Stat(*dbPath)
	if err != nil || !info.Mode().IsRegular() {
		fmt.Fprintln(stderr, "goflexlmweb: --db must name an existing database file")
		return 1
	}
	db, err := store.Open(ctx, *dbPath, store.OpenOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "goflexlmweb: %v\n", err)
		return 1
	}
	defer db.Close()
	handler, err := newDashboard(db, *pool, time.Now)
	if err != nil {
		fmt.Fprintf(stderr, "goflexlmweb: %v\n", err)
		return 1
	}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintf(stderr, "goflexlmweb: listen: %v\n", err)
		return 1
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: time.Minute}
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if server.Shutdown(shutdown) != nil {
				_ = server.Close()
			}
		case <-finished:
		}
	}()
	if _, err := fmt.Fprintf(stdout, "License dashboard: http://%s\n", listener.Addr()); err != nil {
		_ = listener.Close()
		return 1
	}
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(stderr, "goflexlmweb: serve: %v\n", err)
		return 1
	}
	return 0
}
