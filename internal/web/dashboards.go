package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"slices"
	"strings"

	"github.com/labstack/driga/internal/dashboard"
	"github.com/labstack/driga/internal/panel"
	"github.com/labstack/driga/internal/store"
)

// app holds the dashboard host the app opens on, built from ui/ by
// `just ui` and committed so a Go build needs no JavaScript toolchain.
//
//go:embed all:app
var appBuild embed.FS

// you is the author of edits made in the app.
var you = dashboard.Author{Kind: "user"}

func isDashboardPOST(path string) bool { return strings.HasPrefix(path, "/app/api/") }

func (s *Server) appRoutes(mux *http.ServeMux) {
	files, err := fs.Sub(appBuild, "app")
	if err != nil {
		panic(err)
	}
	// One page at /app/, its state in the query; the app's other POST
	// endpoints under /app/ keep answering GET with 405.
	host := http.StripPrefix("/app/", http.FileServer(http.FS(files)))
	mux.HandleFunc("GET /app/{$}", s.appPage)
	mux.Handle("GET /app/assets/", host)
	for _, route := range []string{"list", "open", "run", "create", "edit", "versions", "restore", "delete"} {
		mux.HandleFunc("POST /app/api/"+route, s.dashboardAPI)
	}
}

// appPage serves the app's page with a fresh style nonce: Mantine writes its
// theme variables into a style element, which the policy allows by nonce
// only, so no other inline style runs.
func (s *Server) appPage(w http.ResponseWriter, r *http.Request) {
	page, err := appBuild.ReadFile("app/index.html")
	if err != nil {
		http.Error(w, "The app is not built", http.StatusInternalServerError)
		return
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		http.Error(w, "could not serve the app", http.StatusInternalServerError)
		return
	}
	nonce := base64.StdEncoding.EncodeToString(b)
	w.Header().Set("Content-Security-Policy", strings.Replace(csp, "style-src 'self'", "style-src 'self' 'nonce-"+nonce+"'", 1))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method != http.MethodHead {
		_, _ = w.Write(bytes.Replace(page, []byte("__DRIGA_STYLE_NONCE__"), []byte(nonce), 1))
	}
}

type dashboardRequest struct {
	ID          string                `json:"id,omitempty"`
	Vars        map[string]string     `json:"vars,omitempty"`
	Dashboard   *panel.Dashboard      `json:"dashboard,omitempty"`
	Operations  []dashboard.Operation `json:"operations,omitempty"`
	BaseVersion int                   `json:"base_version,omitempty"`
	Message     string                `json:"message,omitempty"`
	Version     int                   `json:"version,omitempty"`
}

func (s *Server) dashboardAPI(w http.ResponseWriter, r *http.Request) {
	if s.opts.Dashboards == nil {
		http.Error(w, "Dashboards unavailable", http.StatusServiceUnavailable)
		return
	}
	var in dashboardRequest
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "expected application/json", http.StatusUnsupportedMediaType)
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	store := s.opts.Dashboards
	var out any
	var err error
	switch strings.TrimPrefix(r.URL.Path, "/app/api/") {
	case "list":
		var list []dashboard.Summary
		list, err = store.List()
		out = map[string]any{"dashboards": list}
	case "open":
		var rec dashboard.Record
		if rec, err = store.Get(in.ID); err == nil {
			out, err = s.fragment(r.Context(), rec, in.Vars)
		}
	case "run":
		if in.Dashboard == nil {
			err = errors.New("dashboard is required")
			break
		}
		out, err = s.runPanels(r.Context(), panel.RunRequest{Dashboard: *in.Dashboard, Vars: in.Vars})
	case "create":
		if in.Dashboard == nil {
			err = errors.New("dashboard is required")
			break
		}
		var rec dashboard.Record
		if rec, err = store.Create(*in.Dashboard, you, in.Message); err == nil {
			out, err = s.fragment(r.Context(), rec, in.Vars)
		}
	case "edit":
		var rec dashboard.Record
		if rec, err = store.Edit(in.ID, in.Operations, in.BaseVersion, you, in.Message); err == nil {
			out, err = s.fragment(r.Context(), rec, in.Vars)
		}
	case "delete":
		err = store.Delete(in.ID)
		out = map[string]bool{"ok": err == nil}
	case "versions":
		out, err = store.Versions(in.ID)
		out = map[string]any{"versions": out}
	case "restore":
		var rec dashboard.Record
		if rec, err = store.Restore(in.ID, in.Version, in.BaseVersion, you); err == nil {
			out, err = s.fragment(r.Context(), rec, in.Vars)
		}
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, dashboard.ErrNotFound) {
			status = http.StatusNotFound
		} else if errors.Is(err, dashboard.ErrStale) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, out)
}

func (s *Server) fragment(ctx context.Context, rec dashboard.Record, vars map[string]string) (panel.Fragment, error) {
	f, err := s.runPanels(ctx, panel.RunRequest{Dashboard: rec.Spec, Vars: vars})
	f.ID, f.Version = rec.ID, rec.Version
	return f, err
}

// runPanels runs over the same cached snapshot the pages show. Disk
// candidates are read only when a panel shows them.
func (s *Server) runPanels(ctx context.Context, req panel.RunRequest) (panel.Fragment, error) {
	if err := panel.Validate(req.Dashboard); err != nil {
		return panel.Fragment{}, err
	}
	snap, err := s.cachedLoad(ctx, store.Scope{}, false)
	if err != nil {
		return panel.Fragment{}, err
	}
	if s.opts.CleanupList != nil && slices.ContainsFunc(req.Dashboard.Panels, func(p panel.Panel) bool { return p.Source == "cleanup" }) {
		if rows, err := s.cachedCleanup(ctx); err == nil {
			snap.Cleanup = rows
		}
	}
	return panel.Run(snap, req)
}
