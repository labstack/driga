// Package web serves the interactive dashboard, read views and approved Chat actions.
package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/viewer"
)

//go:embed templates/*.html static
var assets embed.FS

const csp = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'"
const cookieName = "driga_token" // prefix; each server appends its port

// Source shares the TUI's consistent view transaction and live snapshot source.
type Source interface {
	LoadScope(context.Context, store.Scope, ...bool) (viewer.Snapshot, error)
	DataVersion(context.Context) (int64, error)
}

type Options struct {
	SessionAction func(context.Context, string, int64) error
	Chat          chat.Client
	ChatEphemeral bool
	Host          string
	Machine       string
	CleanupList   func(context.Context) ([]cleanup.Candidate, error)
	Activity      func(bool)
}

type Server struct {
	routes          *http.ServeMux
	origin          http.CrossOriginProtection
	source          Source
	opts            Options
	token           string
	csrf            string
	chatStreams     chan struct{}
	templates       *template.Template
	static          http.Handler
	themeCSS        string
	mu              sync.Mutex
	clients         map[chan struct{}]bool // Value records a pending data refresh.
	ready           chan struct{}
	cacheMu         sync.Mutex
	cache           map[snapshotKey]cachedSnapshot
	cacheVersion    int64
	cleanupAt       time.Time
	cleanupRows     []cleanup.Candidate
	cleanupErr      error
	cookieName      string
	sseWriteTimeout time.Duration
	watchInterval   time.Duration
	refreshInterval time.Duration
}

// Listen validates before binding; hostnames other than localhost never resolve.
func Listen(address string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("listen requires a loopback address and port: %w", err)
	}
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return nil, fmt.Errorf("listen requires a loopback address: %q", host)
		}
	}
	return net.Listen("tcp", address)
}

func New(source Source, opts Options) (*Server, error) {
	if _, _, err := net.SplitHostPort(opts.Host); err != nil {
		return nil, err
	}
	token := make([]byte, 64)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	t, err := template.New("dashboard").Funcs(templateFuncs()).ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	static, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, err
	}
	s := &Server{
		source: source, opts: opts, token: base64.RawURLEncoding.EncodeToString(token[:32]), csrf: base64.RawURLEncoding.EncodeToString(token[32:]), chatStreams: make(chan struct{}, 32),
		themeCSS: view.ThemeCSS(), templates: t, static: http.StripPrefix("/static/", http.FileServer(http.FS(static))),
		clients: map[chan struct{}]bool{}, ready: make(chan struct{}),
		cache: map[snapshotKey]cachedSnapshot{}, cookieName: instanceCookie(opts.Host), sseWriteTimeout: 10 * time.Second, watchInterval: time.Second, refreshInterval: 15 * time.Second,
	}
	s.routes = s.newRoutes()
	return s, nil
}

func (s *Server) URL() string { return "http://" + s.opts.Host + "/?token=" + s.token }

// Watch checks one connection once a second, regardless of browser count.
func (s *Server) Watch(ctx context.Context) {
	version, err := s.source.DataVersion(ctx)
	close(s.ready)
	initialized := err == nil
	lastRefresh := time.Now()
	ticker := time.NewTicker(s.watchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			next, err := s.source.DataVersion(ctx)
			s.mu.Lock()
			active := len(s.clients) > 0
			s.mu.Unlock()
			if activity, ok := s.source.(interface{ SetActive(bool) }); ok {
				activity.SetActive(active)
			}
			if s.opts.Activity != nil {
				s.opts.Activity(active)
			}
			refresh := err == nil && (!initialized || next != version || time.Since(lastRefresh) >= s.refreshInterval)
			if refresh {
				version, initialized = next, true
				lastRefresh = time.Now()
			}
			s.mu.Lock()
			for ch := range s.clients {
				// Keep a refresh pending if a slow client still has an earlier
				// tick queued. Every tick also schedules that client's tail.
				s.clients[ch] = s.clients[ch] || refresh
				select {
				case ch <- struct{}{}:
				default:
				}
			}
			s.mu.Unlock()
		}
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w = &headerWriter{ResponseWriter: w}
	w.Header().Set("Content-Security-Policy", csp)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodHead && !(r.Method == http.MethodPost && isChatPOST(r.URL.Path)) {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, port, _ := net.SplitHostPort(s.opts.Host)
	if r.Host != s.opts.Host && r.Host != "localhost:"+port {
		http.Error(w, "host not allowed", http.StatusForbidden)
		return
	}
	if r.URL.Query().Has("token") {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "unauthorized", 401)
			return
		}
		if !s.validToken(r.URL.Query().Get("token")) {
			http.Error(w, "unauthorized", 401)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: s.cookieName, Value: s.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		clean := *r.URL
		q := clean.Query()
		q.Del("token")
		clean.RawQuery = q.Encode()
		// Redirect only to an origin-relative path, including for unusual request targets.
		clean.Scheme = ""
		clean.Host = ""
		clean.User = nil
		if strings.HasPrefix(clean.Path, "//") {
			clean.Path = "/"
		}
		http.Redirect(w, r, clean.String(), http.StatusSeeOther)
		return
	}
	cookie, err := r.Cookie(s.cookieName)
	if err != nil || !s.validToken(cookie.Value) {
		http.Error(w, "unauthorized", 401)
		return
	}
	// Match the decoded path, as the dashboard routes have always done.
	if r.URL.RawPath != "" {
		r = r.Clone(r.Context())
		r.URL.RawPath = ""
	}
	// ServeMux redirects noncanonical paths before matching. These routes retain
	// their direct responses (including API errors and static file handling).
	clean := path.Clean(r.URL.Path)
	if clean != r.URL.Path && clean+"/" != r.URL.Path {
		switch {
		case strings.HasPrefix(r.URL.Path, "/chat/api/"):
			s.chatAPI(w, r)
		case strings.HasPrefix(r.URL.Path, "/static/"):
			s.static.ServeHTTP(w, r)
		default:
			s.page(w, r)
		}
		return
	}
	s.routes.ServeHTTP(w, r)
}

func (s *Server) newRoutes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /static", s.page)
	mux.HandleFunc("GET /chat/api", s.page)
	mux.HandleFunc("GET /chat/api/", s.chatAPI)
	for _, path := range []string{"/chat/api/flow-action", "/chat/api/prompt-answer", "/chat/api/new", "/chat/api/send", "/chat/api/stop", "/chat/api/mode", "/chat/api/decide"} {
		mux.HandleFunc("POST "+path, s.chatAPI)
	}
	mux.HandleFunc("GET /static/theme.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		if r.Method != http.MethodHead {
			_, _ = fmt.Fprint(w, s.themeCSS)
		}
	})
	mux.Handle("GET /static/", s.static)
	mux.HandleFunc("GET /flow.json", s.flowJSON)
	mux.HandleFunc("GET /events", s.events)
	mux.HandleFunc("GET /", s.page)
	return mux
}

func (s *Server) validToken(token string) bool {
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) == 1
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	if r.Method == http.MethodHead {
		return
	}
	select {
	case <-s.ready:
	case <-r.Context().Done():
		return
	}
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.clients[ch] = false
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.clients, ch); s.mu.Unlock() }()
	writePayload := func(event, payload string) error {
		rc := http.NewResponseController(w)
		if err := rc.SetWriteDeadline(time.Now().Add(s.sseWriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload); err != nil {
			return err
		}
		return rc.Flush()
	}
	writeEvent := func(event string) error { return writePayload(event, "refresh") }
	seen := map[string]bool{}
	var lastTail time.Time
	sendTail := func() error {
		if !lastTail.IsZero() && time.Since(lastTail) < s.watchInterval {
			return nil
		}
		clone := r.Clone(r.Context())
		u := *r.URL
		u.Path = "/"
		clone.URL = &u
		page, _, err := s.loadPage(clone)
		if err != nil {
			return nil
		} // Keep refreshes alive through a transient read failure.
		pending := []view.TailEvent{}
		nextSeen := map[string]bool{}
		for _, row := range page.Tail {
			nextSeen[row.Key] = true
			if !seen[row.Key] {
				pending = append(pending, row)
			}
		}
		seen = nextSeen
		// Flood policy: retain the newest twenty per second; no unbounded client queue.
		if len(pending) > 20 {
			pending = pending[len(pending)-20:]
		}
		if len(pending) == 0 {
			return nil
		}
		data, err := json.Marshal(pending)
		if err != nil {
			return err
		}
		if err := writePayload("tail", string(data)); err != nil {
			return err
		}
		lastTail = time.Now()
		return nil
	}
	if err := writeEvent("ready"); err != nil {
		return
	}
	if err := sendTail(); err != nil {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			s.mu.Lock()
			refresh := s.clients[ch]
			s.clients[ch] = false
			s.mu.Unlock()
			if refresh {
				if err := writeEvent("refresh"); err != nil {
					return
				}
				if err := writeEvent("change"); err != nil {
					return
				}
			}
			if err := sendTail(); err != nil {
				return
			}
		}
	}
}

func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	p, status, err := s.loadPage(r)
	if err != nil {
		http.Error(w, err.Error(), status)
		return
	}
	name := "page"
	w.Header().Set("Vary", "HX-Request")
	if r.Header.Get("HX-Request") == "true" {
		name = "content"
	}
	var buf bytes.Buffer
	if err := s.templates.ExecuteTemplate(&buf, name, p); err != nil {
		http.Error(w, "could not render dashboard", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method != http.MethodHead {
		_, _ = w.Write(buf.Bytes())
	}
}

func instanceCookie(host string) string {
	_, port, _ := net.SplitHostPort(host)
	return cookieName + "_" + port
}

type headerWriter struct {
	http.ResponseWriter
	wrote bool
}

func (w *headerWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *headerWriter) WriteHeader(code int) {
	if w.wrote {
		return
	}
	w.wrote = true
	w.Header().Set("Content-Security-Policy", csp)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	w.ResponseWriter.WriteHeader(code)
}
func (w *headerWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// flowJSON shares page scope validation and the exact CLI/MCP FlowView contract.
func (s *Server) flowJSON(w http.ResponseWriter, r *http.Request) {
	clone := r.Clone(r.Context())
	u := *r.URL
	if r.URL.Query().Get("view") == "board" {
		u.Path = "/flow"
	} else {
		u.Path = "/flow.json"
	}
	clone.URL = &u
	if u.Path == "/flow.json" {
		u.Path = "/flow"
		q := u.Query()
		q.Set("view", "family")
		u.RawQuery = q.Encode()
	}
	p, status, err := s.loadPage(clone)
	if err != nil {
		http.Error(w, err.Error(), status)
		return
	}
	e := p.Snapshot.Envelope
	e.Schema = 2
	e.ObservedAt = p.Flow.ObservedAt
	e.Data = p.Flow
	if p.IsBoard {
		e.ObservedAt = p.Board.ObservedAt
		e.Data = p.Board
	}
	raw, err := json.Marshal(e)
	if err != nil {
		http.Error(w, "could not encode flow", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Method != http.MethodHead {
		_, _ = w.Write(raw)
	}
}
