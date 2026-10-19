// Package web serves the read-only browser dashboard.
package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/viewer"
)

//go:embed templates/*.html static
var assets embed.FS

const csp = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'"
const cookieName = "driga_token"

// Source shares the TUI's consistent view transaction and live snapshot source.
type Source interface {
	LoadScope(context.Context, store.Scope, ...bool) (viewer.Snapshot, error)
	DataVersion(context.Context) (int64, error)
}

type Options struct {
	Host        string
	Machine     string
	CleanupList func(context.Context) ([]cleanup.Candidate, error)
}

type Server struct {
	source    Source
	opts      Options
	token     string
	templates *template.Template
	static    http.Handler
	themeCSS  string
	mu        sync.Mutex
	clients   map[chan struct{}]bool
	ready     chan struct{}
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
	token := make([]byte, 32)
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
	return &Server{
		source: source, opts: opts, token: base64.RawURLEncoding.EncodeToString(token),
		themeCSS: view.ThemeCSS(), templates: t, static: http.StripPrefix("/static/", http.FileServer(http.FS(static))),
		clients: map[chan struct{}]bool{}, ready: make(chan struct{}),
	}, nil
}

func (s *Server) URL() string { return "http://" + s.opts.Host + "/?token=" + s.token }

// Watch checks one connection once a second, regardless of browser count.
func (s *Server) Watch(ctx context.Context) {
	version, err := s.source.DataVersion(ctx)
	close(s.ready)
	initialized := err == nil
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			next, err := s.source.DataVersion(ctx)
			if err != nil {
				continue
			}
			if initialized && next == version {
				continue
			}
			version, initialized = next, true
			s.mu.Lock()
			for ch := range s.clients {
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
	w.Header().Set("Content-Security-Policy", csp)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
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
		if !s.validToken(r.URL.Query().Get("token")) {
			http.Error(w, "unauthorized", 401)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: s.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
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
	cookie, err := r.Cookie(cookieName)
	if err != nil || !s.validToken(cookie.Value) {
		http.Error(w, "unauthorized", 401)
		return
	}
	if r.URL.Path == "/static/theme.css" {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		if r.Method != http.MethodHead {
			_, _ = fmt.Fprint(w, s.themeCSS)
		}
		return
	}
	if strings.HasPrefix(r.URL.Path, "/static/") {
		s.static.ServeHTTP(w, r)
		return
	}
	if r.URL.Path == "/events" {
		s.events(w, r)
		return
	}
	s.page(w, r)
}

func (s *Server) validToken(token string) bool {
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) == 1
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	if r.Method == http.MethodHead {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unavailable", 500)
		return
	}
	select {
	case <-s.ready:
	case <-r.Context().Done():
		return
	}
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.clients[ch] = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.clients, ch); s.mu.Unlock() }()
	if _, err := fmt.Fprint(w, "event: ready\ndata: refresh\n\n"); err != nil {
		return
	}
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			if _, err := fmt.Fprint(w, "event: change\ndata: refresh\n\n"); err != nil {
				return
			}
			flusher.Flush()
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
