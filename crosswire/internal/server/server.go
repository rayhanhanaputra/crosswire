// Package server is the Crosswire workbench: the JSON API, the sink, and
// the embedded web UI, bound to loopback.
package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/channel"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/engine"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/model"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/sink"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/store"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/tts"
)

//go:embed ui
var uiFiles embed.FS

const maxBody = 32 << 20

// Server holds the workbench state shared by all handlers.
type Server struct {
	version     string
	dataDir     string
	sinkURL     string
	allowRemote bool
	base        context.Context
	st          *store.Store
	sink        *sink.Sink
	enc         channel.Encoder
	jobs        *jobs
}

// New builds a server. base bounds background (async) runs.
func New(base context.Context, st *store.Store, version, dataDir string, allowRemote bool) *Server {
	return &Server{
		version: version, dataDir: dataDir, allowRemote: allowRemote, base: base,
		st: st, sink: sink.New(), enc: channel.Encoder{Speak: tts.Speak}, jobs: newJobs(),
	}
}

// Main runs the server with the given CLI args.
func Main(version string, args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	listen := flags.String("listen", "127.0.0.1:7117", "address to listen on (loopback only unless --allow-remote)")
	data := flags.String("data", defaultData(), "data directory")
	allowRemote := flags.Bool("allow-remote", false, "allow a non-loopback listen address (the API has no authentication)")
	open := flags.Bool("open", false, "print the URL to open")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := checkListen(*listen, *allowRemote); err != nil {
		return err
	}
	if err := os.MkdirAll(*data, 0o700); err != nil {
		return fmt.Errorf("data dir: %w", err)
	}
	st, err := store.Open(filepath.Join(*data, "crosswire.db"))
	if err != nil {
		return err
	}
	defer st.Close()
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	s := New(ctx, st, version, *data, *allowRemote)
	siteURL := "http://" + ln.Addr().String()
	s.sinkURL = siteURL + "/api/sink"
	log.Printf("crosswire %s: ui %s, sink %s, data %s", version, siteURL, s.sinkURL, *data)
	if *allowRemote {
		log.Printf("warning: --allow-remote set; the API has no authentication")
	}
	if *open {
		fmt.Println(siteURL)
	}

	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		_ = srv.Close()
	}
	return nil
}

func defaultData() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".crosswire"
	}
	return filepath.Join(home, ".crosswire")
}

// checkListen refuses a non-loopback address: the API starts runs against
// arbitrary endpoints and has no authentication.
func checkListen(addr string, allowRemote bool) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("listen address %q: %w", addr, err)
	}
	if allowRemote || isLoopbackHost(host) {
		return nil
	}
	return fmt.Errorf("refusing to listen on %q: not a loopback address (use --allow-remote to override)", addr)
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// Handler returns the full HTTP handler (API, sink and UI).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/channels", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, channel.Catalogue()) })

	mux.HandleFunc("GET /api/targets", s.listTargets)
	mux.HandleFunc("POST /api/targets", s.createTarget)
	mux.HandleFunc("GET /api/targets/{id}", s.getTarget)
	mux.HandleFunc("PUT /api/targets/{id}", s.updateTarget)
	mux.HandleFunc("DELETE /api/targets/{id}", s.deleteTarget)

	mux.HandleFunc("POST /api/preview", s.handlePreview)
	mux.HandleFunc("POST /api/runs", s.createRun)
	mux.HandleFunc("GET /api/runs", s.listRuns)
	mux.HandleFunc("GET /api/runs/{id}", s.getRun)
	mux.HandleFunc("GET /api/runs/{id}/events", s.runEvents)
	mux.HandleFunc("GET /api/runs/{id}/artifacts/{arm}/{index}", s.getArtifact)
	mux.HandleFunc("GET /api/runs/{id}/artifacts/{arm}/{index}/reply", s.getReplyArtifact)
	mux.HandleFunc("GET /api/runs/{id}/bundle", s.getBundle)

	mux.HandleFunc("POST /api/intruder", s.createIntruder)
	mux.HandleFunc("GET /api/intruder/{id}/events", s.intruderEvents)

	mux.HandleFunc("POST /api/sink", s.sink.Handler())
	mux.HandleFunc("GET /api/sink", s.listSink)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeErr(w, 404, errors.New("no such endpoint")) })

	ui, _ := fs.Sub(uiFiles, "ui")
	mux.Handle("/", http.FileServer(http.FS(ui)))
	return s.guard(mux)
}

// guard enforces the same-origin model: with no authentication, a web page
// the operator happens to visit must not be able to drive the API, either by
// a cross-origin request or by DNS rebinding to 127.0.0.1.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.allowRemote {
			host := r.Host
			if h, _, err := net.SplitHostPort(r.Host); err == nil {
				host = h
			}
			if !isLoopbackHost(host) {
				writeErr(w, http.StatusForbidden, errors.New("host not allowed"))
				return
			}
		}
		if o := r.Header.Get("Origin"); o != "" {
			if u, err := url.Parse(o); err != nil || u.Host != r.Host {
				writeErr(w, http.StatusForbidden, errors.New("cross-origin request refused"))
				return
			}
		}
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; media-src 'self' data:; frame-ancestors 'none'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"status": "ok", "version": s.version, "sink_url": s.sinkURL, "data_dir": s.dataDir})
}

func (s *Server) listSink(w http.ResponseWriter, r *http.Request) {
	evs := s.sink.Recent(limitParam(r, 100, 10000))
	if evs == nil {
		evs = []model.SinkEvent{}
	}
	writeJSON(w, 200, evs)
}

func (s *Server) engine(onTrial func(string, model.Trial)) *engine.Engine {
	return &engine.Engine{Store: s.st, Sink: s.sink, Encoder: s.enc, OnTrial: onTrial}
}

func limitParam(r *http.Request, def, ceiling int) int {
	n := def
	if v := r.URL.Query().Get("limit"); v != "" {
		if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 {
			n = def
		}
	}
	return min(n, ceiling)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// decode reads a JSON body; an oversize body gets 413, anything else 400.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeErr(w, http.StatusRequestEntityTooLarge, errors.New("request body exceeds 32 MiB"))
		} else {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err))
		}
		return false
	}
	return true
}
