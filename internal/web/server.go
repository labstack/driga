// Package web serves the app: its dashboards, Chat and the local actions
// both run through the app's checks.
package web

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/dashboard"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/viewer"
)

// static holds the fonts and brand images the app's page loads.
//
//go:embed static
var assets embed.FS

const csp = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'"

// Source shares the TUI's consistent view transaction and live snapshot source.
type Source interface {
	LoadScope(context.Context, store.Scope, ...bool) (viewer.Snapshot, error)
	DataVersion(context.Context) (int64, error)
}

type Options struct {
	// Action runs a local action, as Chat's driga tools do: jump, kill,
	// cleanup, ack_inbox or summarize, with their JSON arguments. Without it
	// the endpoint answers 503.
	Action      func(ctx context.Context, name string, args json.RawMessage) error
	Chat        chat.Client
	CleanupList func(context.Context) ([]cleanup.Candidate, error)
	// Activity reports whether the app is open and polling.
	Activity func(bool)
	// Confirm and OpenExternal are the host's native dialog and browser
	// opener; without them their endpoints answer 404.
	Confirm      func(ctx context.Context, message string) bool
	OpenExternal func(url string)
	// Dashboards enables the dashboard host at /app/ and its API.
	Dashboards *dashboard.Store
}

// Server is a transport-free handler: the app's page, its JSON endpoints and
// Chat's long polls.
type confirmation struct {
	key   string
	until time.Time
}

type Server struct {
	confirmations map[string]confirmation
	livePrompts   []chat.Prompt // last good live hooks; memory only
	routes        *http.ServeMux
	source        Source
	opts          Options
	static        http.Handler
	mu            sync.Mutex
	waiters       int       // Chat long polls open now.
	polled        time.Time // The app's last request.
	cacheMu       sync.Mutex
	cache         map[snapshotKey]cachedSnapshot
	snapshotLoads map[snapshotKey]*snapshotLoad
	cleanupAt     time.Time
	cleanupRows   []cleanup.Candidate
	cleanupLoad   *cleanupLoad
	watchInterval time.Duration
	pollTimeout   time.Duration
}

// activeFor is how long the app counts as open after its last request; it
// refreshes every five seconds while visible.
const activeFor = 12 * time.Second

func New(source Source, opts Options) (*Server, error) {
	static, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, err
	}
	s := &Server{
		source: source, opts: opts, static: http.StripPrefix("/static/", http.FileServer(http.FS(static))),
		confirmations: map[string]confirmation{}, cache: map[snapshotKey]cachedSnapshot{}, watchInterval: time.Second, pollTimeout: 25 * time.Second,
	}
	s.routes = s.newRoutes()
	return s, nil
}

// Watch reports once a second whether the app is open, so the source and
// the collector can read fresh data only while someone is looking.
func (s *Server) Watch(ctx context.Context) {
	ticker := time.NewTicker(s.watchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			active := s.waiters > 0 || time.Since(s.polled) < activeFor
			s.mu.Unlock()
			if activity, ok := s.source.(interface{ SetActive(bool) }); ok {
				activity.SetActive(active)
			}
			if s.opts.Activity != nil {
				s.opts.Activity(active)
			}
		}
	}
}

// seen records a request from the app.
func (s *Server) seen() {
	s.mu.Lock()
	s.polled = time.Now()
	s.mu.Unlock()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w = &headerWriter{ResponseWriter: w}
	w.Header().Set("Content-Security-Policy", csp)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	if !appRequestOrigin(r) {
		http.Error(w, "Untrusted app origin", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && !(r.Method == http.MethodPost && (isChatPOST(r.URL.Path) || isAppPOST(r.URL.Path) || isDashboardPOST(r.URL.Path))) {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/app/") || strings.HasPrefix(r.URL.Path, "/chat/api/") {
		s.seen()
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
		if strings.HasPrefix(r.URL.Path, "/chat/api/") {
			s.chatAPI(w, r)
		} else {
			http.NotFound(w, r)
		}
		return
	}
	s.routes.ServeHTTP(w, r)
}

func (s *Server) newRoutes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /chat/api/", s.chatAPI)
	for _, path := range []string{"/chat/api/prompt-answer", "/chat/api/new", "/chat/api/send", "/chat/api/stop", "/chat/api/mode", "/chat/api/decide"} {
		mux.HandleFunc("POST "+path, s.chatAPI)
	}
	s.appRoutes(mux)
	mux.HandleFunc("POST /app/confirm", s.appConfirm)
	mux.HandleFunc("POST /app/action", s.appAction)
	mux.HandleFunc("POST /app/open-external", s.appOpenExternal)
	mux.Handle("GET /static/", s.static)
	// Without these, the mux would redirect to the directory.
	mux.HandleFunc("GET /static", http.NotFound)
	mux.HandleFunc("GET /chat/api", http.NotFound)
	// The app is the only page.
	mux.Handle("GET /{$}", http.RedirectHandler("/app/", http.StatusFound))
	return mux
}

func isAppPOST(path string) bool {
	return path == "/app/confirm" || path == "/app/action" || path == "/app/open-external"
}

// decodeAppJSON reads one small JSON object from the page.
func decodeAppJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "expected application/json", http.StatusUnsupportedMediaType)
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// appConfirm asks the host's native dialog in place of the page's confirm().
func (s *Server) appConfirm(w http.ResponseWriter, r *http.Request) {
	if s.opts.Confirm == nil {
		http.NotFound(w, r)
		return
	}
	var in struct {
		Message string         `json:"message"`
		Request *actionRequest `json:"request,omitempty"`
	}
	if !decodeAppJSON(w, r, &in) {
		return
	}
	if in.Request != nil {
		snap, err := s.cachedLoad(r.Context(), store.Scope{}, false)
		if err != nil {
			http.Error(w, "Snapshot unavailable", http.StatusServiceUnavailable)
			return
		}
		if _, err := actionArgs(*in.Request, snap); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// The native prompt includes the exact request the issued token authorizes.
		in.Message += "\nAction: " + confirmationKey(*in.Request)
	}
	confirmed := s.opts.Confirm(r.Context(), in.Message)
	token := ""
	if confirmed && in.Request != nil {
		var raw [32]byte
		if _, err := rand.Read(raw[:]); err != nil {
			http.Error(w, "Confirmation unavailable", http.StatusServiceUnavailable)
			return
		}
		token = hex.EncodeToString(raw[:])
		s.mu.Lock()
		now := time.Now()
		for key, c := range s.confirmations {
			if !now.Before(c.until) {
				delete(s.confirmations, key)
			}
		}
		if len(s.confirmations) >= 128 {
			s.mu.Unlock()
			http.Error(w, "Too many pending confirmations", http.StatusTooManyRequests)
			return
		}
		s.confirmations[token] = confirmation{confirmationKey(*in.Request), now.Add(time.Minute)}
		s.mu.Unlock()
	}
	writeJSON(w, struct {
		Confirmed bool   `json:"confirmed"`
		Token     string `json:"token,omitempty"`
	}{confirmed, token})
}

// appOpenExternal hands a web link to the default browser.
func (s *Server) appOpenExternal(w http.ResponseWriter, r *http.Request) {
	if s.opts.OpenExternal == nil {
		http.NotFound(w, r)
		return
	}
	var in struct {
		URL string `json:"url"`
	}
	if !decodeAppJSON(w, r, &in) {
		return
	}
	u, err := url.Parse(in.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		http.Error(w, "expected an http or https URL", http.StatusBadRequest)
		return
	}
	s.opts.OpenExternal(u.String())
	writeJSON(w, map[string]bool{"ok": true})
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
	// A page with its own policy, such as the app's nonce, keeps it.
	if w.Header().Get("Content-Security-Policy") == "" {
		w.Header().Set("Content-Security-Policy", csp)
	}
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

// HTTP is loopback-only; POSTs must originate at the same authority. The
// toolkit's private protocol identifies requests by its transport address.
func appRequestOrigin(r *http.Request) bool {
	scheme := r.URL.Scheme
	if scheme == "" {
		scheme = "http"
		if r.TLS != nil {
			scheme = "https"
		}
	}
	origin := r.Header.Get("Origin")
	if scheme == "mygo" {
		// MyGo stamps this transport address itself, after translating
		// WebView2's http://mygo.localhost URL back to mygo://localhost.
		// Its original Origin header remains unchanged on Windows.
		if !strings.HasPrefix(r.RemoteAddr, "mygo:") || r.Host != "localhost" {
			return false
		}
		if r.Header.Values("Sec-Fetch-Site") != nil {
			if site := r.Header.Get("Sec-Fetch-Site"); site != "same-origin" && site != "none" {
				return false
			}
		}
		// WKWebView omits Origin for fetches over the private scheme.
		return origin == "" || origin == "mygo://localhost" || origin == "http://mygo.localhost"
	}
	if scheme != "http" && scheme != "https" {
		return false
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" && host != "[::1]" {
		return false
	}
	if r.Method != http.MethodPost && origin == "" {
		return true
	}
	return origin == scheme+"://"+r.Host
}
