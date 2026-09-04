package main

import (
	"cmp"
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"
)

var (
	revision       = "unknown"
	buildTimestamp = "unknown"
)

//go:embed templates assets
var content embed.FS

var (
	t *template.Template

	// TODO: track number of clients/user and show a stats page below which also refreshes
)

func init() {
	t = template.Must(template.ParseFS(content, "templates/*.gohtml"))
}

type config struct {
	Addr            string
	NumCells        uint
	RefreshInterval time.Duration
}

func main() {
	revision = cmp.Or(os.Getenv("REVISION"), revision, "unknown")
	buildTimestamp = cmp.Or(os.Getenv("BUILD_TS"), buildTimestamp, "unknown")

	var cfg config
	fs := flag.NewFlagSet("datastar-cgol-go", flag.ExitOnError)
	fs.StringVar(&cfg.Addr, "addr", "localhost:8000", "address for the server to listen on, in the form `host:port`")
	fs.UintVar(&cfg.NumCells, "num-cells", 2500, "number of cells")
	fs.DurationVar(&cfg.RefreshInterval, "refresh-int", 200*time.Millisecond, "refresh interval")

	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
		return
	}

	if cfg.NumCells%2 == 1 {
		fmt.Fprintln(os.Stderr, "num-cells must be dividable by 2 without rest")
		os.Exit(1)
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	r := http.NewServeMux()

	files := http.FileServer(http.FS(content))
	r.Handle("/assets/", files)

	r.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		logger.Info("new client connected", "ua", r.Header.Get("User-Agent"))

		data := map[string]any{
			"runtime_GOOS":    runtime.GOOS,
			"runtime_GOARCH":  runtime.GOARCH,
			"runtime_version": runtime.Version(),
			"revision":        revision,
			"buildTS":         buildTimestamp,
		}

		err := t.ExecuteTemplate(w, "index.gohtml", data)
		if err != nil {
			logger.Error(r.Pattern, "err", err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	s := &http.Server{
		Addr:    cfg.Addr,
		Handler: r,
	}

	logger.Info(fmt.Sprintf("running on %s/%s with %s on revision %s built on %s",
		runtime.GOOS, runtime.GOARCH, runtime.Version(), revision, buildTimestamp))

	go func(server *http.Server, logger *slog.Logger) {
		logger.Info("Listening on " + cfg.Addr)

		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("ListenAndServe error", "err", err.Error())
		}
	}(s, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, os.Kill)
	<-ctx.Done()
	defer stop()

	logger.Warn("Shutting down", "err", ctx.Err().Error(), "cause", context.Cause(ctx))

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// TODO close all SSE streams

	if err := s.Shutdown(ctx); err != nil {
		logger.Error("error shutting down", "err", err.Error())
	}
	logger.Info("all connections closed, shutdown complete")
}
