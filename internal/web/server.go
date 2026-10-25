// Package web serves the interactive dashboard, read views and approved Chat actions.
package web

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
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

// Source shares the TUI's consistent view transaction and live snapshot source.
type Source interface {
	LoadScope(context.Context, store.Scope, ...bool) (viewer.Snapshot, error)
	DataVersion(context.Context) (int64, error)
}

type Options struct {
	SessionAction func(context.Context, string, int64) error
	Chat          chat.Client
	ChatEphemeral bool
	Machine       string
	CleanupList   func(context.Context) ([]cleanup.Candidate, error)
	Activity      func(bool)
	// Confirm and OpenExternal are the host's native dialog and browser
	// opener; without them their endpoints answer 404.
	Confirm      func(ctx context.Context, message string) bool
	OpenExternal func(url string)
	// Theme is the app's ui.theme: "system", "light" or "dark".
	Theme string
}

// Server is a transport-free handler: pages, their JSON endpoints and long polls.
type Server struct {
	routes          *http.ServeMux
	source          Source
	opts            Options
	templates       *template.Template
	static          http.Handler
	themeCSS        string
	mu              sync.Mutex
	generation      uint64        // Advances on every page refresh.
	tick            chan struct{} // Closed and replaced on every watch tick.
	waiters         int
	polled          time.Time
	ready           chan struct{}
	cacheMu         sync.Mutex
	cache           map[snapshotKey]cachedSnapshot
	cacheVersion    int64
	cleanupAt       time.Time
	cleanupRows     []cleanup.Candidate
	cleanupErr      error
	watchInterval   time.Duration
	refreshInterval time.Duration
	pollTimeout     time.Duration
}

func New(source Source, opts Options) (*Server, error) {
	t, err := template.New("dashboard").Funcs(templateFuncs()).ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	static, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, err
	}
	s := &Server{
		source: source, opts: opts, themeCSS: view.ThemeCSS(), templates: t, static: http.StripPrefix("/static/", http.FileServer(http.FS(static))),
		// A cursor from an earlier server never matches this one's generation.
		generation: uint64(time.Now().UnixNano()), tick: make(chan struct{}), ready: make(chan struct{}),
		cache: map[snapshotKey]cachedSnapshot{}, watchInterval: time.Second, refreshInterval: 15 * time.Second, pollTimeout: 25 * time.Second,
	}
	s.routes = s.newRoutes()
	return s, nil
}

// Watch checks one connection once a second, regardless of page count, and
// wakes every waiting poll.
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
			// A page polls again as soon as an answer arrives; the grace
			// period keeps that gap from reading as the page leaving.
			active := s.waiters > 0 || time.Since(s.polled) < 2*s.watchInterval
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
			if refresh {
				s.generation++
			}
			close(s.tick)
			s.tick = make(chan struct{})
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
	if r.Method != http.MethodGet && r.Method != http.MethodHead && !(r.Method == http.MethodPost && (isChatPOST(r.URL.Path) || isAppPOST(r.URL.Path))) {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
	mux.HandleFunc("POST /app/confirm", s.appConfirm)
	mux.HandleFunc("POST /app/open-external", s.appOpenExternal)
	mux.HandleFunc("GET /static/theme.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		if r.Method != http.MethodHead {
			_, _ = fmt.Fprint(w, s.themeCSS)
		}
	})
	mux.Handle("GET /static/", s.static)
	mux.HandleFunc("GET /flow.json", s.flowJSON)
	mux.HandleFunc("GET /updates", s.updates)
	mux.HandleFunc("GET /", s.page)
	return mux
}

// seenKeys bounds the tail keys a cursor remembers.
const seenKeys = 64

// shortKey is the part of a tail key a cursor keeps: tail keys are SHA-256
// hex, and sixteen digits tell a page's rows apart.
func shortKey(key string) string { return key[:min(len(key), 16)] }

// tailMark is what a page holds of the tail: the newest row in the tail's
// (time, key) order, and the keys of the newest rows at or before it. A row
// at or after floor that the page has not seen is sent even when it is older
// than the newest, as when a machine's events arrive late; older rows count
// as seen, which keeps the remembered keys bounded.
type tailMark struct {
	at    int64
	key   string // Empty before the first row.
	floor int64
	seen  []string // Short keys, oldest first.
}

func (m tailMark) newer(row view.TailEvent) bool {
	at := row.TS.UnixNano()
	return m.key == "" || at > m.at || at == m.at && row.Key > m.key
}

// unseen reports whether the page lacks row.
func (m tailMark) unseen(row view.TailEvent) bool {
	if m.newer(row) {
		return true
	}
	return row.TS.UnixNano() >= m.floor && !slices.Contains(m.seen, shortKey(row.Key))
}

// covering returns the mark once the page holds tail up to newest: every row
// of tail not newer than newest is then sent or deliberately skipped.
func (m tailMark) covering(tail []view.TailEvent, newest tailMark) tailMark {
	held := []view.TailEvent{}
	for _, row := range tail {
		if !newest.newer(row) {
			held = append(held, row)
		}
	}
	next := tailMark{at: newest.at, key: newest.key, floor: m.floor}
	if len(held) > seenKeys {
		cut := len(held) - seenKeys
		floor := held[cut].TS.UnixNano()
		if held[cut-1].TS.UnixNano() == floor {
			// A forgotten row shares the oldest kept time: start after it.
			floor++
		}
		next.floor = max(next.floor, floor)
		held = held[cut:]
	}
	for _, row := range held {
		if row.TS.UnixNano() >= next.floor {
			next.seen = append(next.seen, shortKey(row.Key))
		}
	}
	return next
}

// The cursor is opaque to the page: refresh generation and tail mark. The
// key goes last, so any character in it survives.
func encodeCursor(generation uint64, mark tailMark) string {
	return fmt.Sprintf("%d.%d.%d.%s.%s", generation, mark.at, mark.floor, strings.Join(mark.seen, ","), mark.key)
}

func decodeCursor(raw string) (uint64, tailMark, error) {
	parts := strings.SplitN(raw, ".", 5)
	if len(parts) != 5 {
		return 0, tailMark{}, fmt.Errorf("invalid cursor")
	}
	generation, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, tailMark{}, fmt.Errorf("invalid cursor")
	}
	at, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, tailMark{}, fmt.Errorf("invalid cursor")
	}
	floor, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return 0, tailMark{}, fmt.Errorf("invalid cursor")
	}
	mark := tailMark{at: at, key: parts[4], floor: floor}
	if parts[3] != "" {
		mark.seen = strings.Split(parts[3], ",")
		if len(mark.seen) > seenKeys {
			return 0, tailMark{}, fmt.Errorf("invalid cursor")
		}
	}
	return generation, mark, nil
}

type update struct {
	Cursor  string           `json:"cursor"`
	Refresh bool             `json:"refresh"`
	Tail    []view.TailEvent `json:"tail"`
}

// updates answers one long poll. A first call returns the cursor and current
// tail at once. Later calls return when the page should refresh, when a watch
// tick finds tail rows past the cursor, or empty after pollTimeout. Ticks pace
// the answers, so a page receives at most one tail batch per watch interval.
func (s *Server) updates(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	var generation uint64
	var mark tailMark
	first := !r.URL.Query().Has("after")
	if !first {
		var err error
		if generation, mark, err = decodeCursor(r.URL.Query().Get("after")); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	select {
	case <-s.ready:
	case <-r.Context().Done():
		return
	}
	s.mu.Lock()
	s.waiters++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.waiters--
		s.polled = time.Now()
		s.mu.Unlock()
	}()
	timeout := time.NewTimer(s.pollTimeout)
	defer timeout.Stop()
	for {
		s.mu.Lock()
		current, tick := s.generation, s.tick
		s.mu.Unlock()
		if first || current != generation {
			rows, next := s.tailSince(r, mark)
			writeUpdate(w, update{Cursor: encodeCursor(current, next), Refresh: !first, Tail: rows})
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-timeout.C:
			writeUpdate(w, update{Cursor: encodeCursor(generation, mark), Tail: []view.TailEvent{}})
			return
		case <-tick:
			s.mu.Lock()
			current = s.generation
			s.mu.Unlock()
			if current != generation {
				continue
			}
			if rows, next := s.tailSince(r, mark); len(rows) > 0 {
				writeUpdate(w, update{Cursor: encodeCursor(generation, next), Tail: rows})
				return
			}
		}
	}
}

// tailSince reads the overview tail in the page's scope and returns the rows
// past mark, with the mark that covers them.
func (s *Server) tailSince(r *http.Request, mark tailMark) ([]view.TailEvent, tailMark) {
	clone := r.Clone(r.Context())
	u := *r.URL
	u.Path = "/"
	clone.URL = &u
	page, _, err := s.loadPage(clone)
	if err != nil {
		return []view.TailEvent{}, mark
	} // Keep refreshes alive through a transient read failure.
	rows := []view.TailEvent{}
	newest := mark
	for _, row := range page.Tail {
		if mark.unseen(row) {
			rows = append(rows, row)
			if newest.newer(row) {
				newest = tailMark{at: row.TS.UnixNano(), key: row.Key}
			}
		}
	}
	next := mark.covering(page.Tail, newest)
	// Flood policy: retain the newest twenty per answer; no unbounded page queue.
	if len(rows) > 20 {
		rows = rows[len(rows)-20:]
	}
	return rows, next
}

func writeUpdate(w http.ResponseWriter, u update) {
	raw, err := json.Marshal(u)
	if err != nil {
		http.Error(w, "could not encode update", 500)
		return
	}
	_, _ = w.Write(raw)
}

func isAppPOST(path string) bool { return path == "/app/confirm" || path == "/app/open-external" }

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
		Message string `json:"message"`
	}
	if !decodeAppJSON(w, r, &in) {
		return
	}
	writeJSON(w, map[string]bool{"confirmed": s.opts.Confirm(r.Context(), in.Message)})
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
