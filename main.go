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
	"strconv"
	"syscall"
	"time"

	"github.com/starfederation/datastar-go/datastar"
	"github.com/valyala/bytebufferpool"
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
	fs.UintVar(&cfg.NumCells, "num-cells", 2500, "number of cells, will be rounded down if not sqrt'able")
	fs.DurationVar(&cfg.RefreshInterval, "refresh-int", 200*time.Millisecond, "refresh interval")

	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	g := NewGame(cfg.NumCells, cfg.RefreshInterval, logger)
	go g.Start(ctx)

	r := http.NewServeMux()

	files := http.FileServer(http.FS(content))
	r.Handle("/assets/", files)

	r.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
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
			return
		}
	})

	r.HandleFunc("POST /{$}", func(w http.ResponseWriter, r *http.Request) {
		s := g.Sub()
		defer g.Unsub(s)
		logger.Info(fmt.Sprintf("sub %v created", s))

		sse := datastar.NewSSE(w, r, datastar.WithCompression(datastar.WithBrotli()))

		for {
			select {
			case <-r.Context().Done():
				logger.Info(fmt.Sprintf("sub %v left", s), "err", r.Context().Err())
				return
			case board := <-s.StateCh:
				err := patchTemplate(sse, "gameboard", board)
				if err != nil {
					logger.Error(r.Pattern, "err", err.Error())
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
		}
	})

	r.HandleFunc("POST /tap", func(w http.ResponseWriter, r *http.Request) {
		// Note that errors are explicit ignored and made a NOOP, hence the return of http.StatusNoContent
		x, err := strconv.Atoi(r.URL.Query().Get("x"))
		if err != nil {
			logger.Error(r.Pattern, "err", err)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		y, err := strconv.Atoi(r.URL.Query().Get("y"))
		if err != nil {
			logger.Error(r.Pattern, "err", err)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		logger.Info(r.Pattern, "x", x, "y", y)

		err = g.Set(x, y)
		if err != nil {
			logger.Error(r.Pattern, "err", err)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		w.WriteHeader(http.StatusNoContent)
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

	<-ctx.Done()
	logger.Warn("Shutting down", "err", ctx.Err().Error(), "cause", context.Cause(ctx))

	tCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err := s.Shutdown(tCtx); err != nil {
		logger.Error("error shutting down", "err", err.Error())
	}
	logger.Info("all connections closed, shutdown complete")
}

func patchTemplate[T any](sse *datastar.ServerSentEventGenerator, tpl string, data T) error {
	buf := bytebufferpool.Get()
	defer bytebufferpool.Put(buf)

	if err := t.ExecuteTemplate(buf, tpl, data); err != nil {
		return fmt.Errorf("failed to execute template %q: %w", tpl, err)
	}

	return sse.PatchElements(buf.String()) // datastar.WithViewTransitions() ?
}
