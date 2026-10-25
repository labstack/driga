// Package dashboard stores the user's dashboards as versioned files in the
// data directory. People edit them in the app; agents edit them through MCP.
// Every save is a new version, and restoring an old one saves it again.
package dashboard

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/labstack/driga/internal/filelock"
	"github.com/labstack/driga/internal/panel"
	"github.com/labstack/driga/internal/secfile"
)

// maxVersions bounds a dashboard's history; the oldest versions drop first.
const maxVersions = 50

var (
	ErrNotFound      = errors.New("dashboard not found")
	ErrStale         = errors.New("dashboard changed since that version; read it again")
	ErrVersion       = errors.New("version not found")
	ErrAlreadyLatest = errors.New("that version is already current")
	idPattern        = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
)

// Author is who saved a version: the person in the app, or an agent.
type Author struct {
	Kind string `json:"kind"` // user or agent
	Name string `json:"name,omitempty"`
}

// Version is one saved spec.
type Version struct {
	Version int             `json:"version"`
	SavedAt time.Time       `json:"saved_at"`
	Author  Author          `json:"author"`
	Message string          `json:"message,omitempty"`
	Spec    panel.Dashboard `json:"spec"`
}

// Record is a dashboard's current spec and its history.
type Record struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Version int       `json:"version"`
	SavedAt time.Time `json:"saved_at,omitzero"`
	Spec    panel.Dashboard
	history []Version
}

// MarshalJSON leaves the history out; Versions reads it.
func (r Record) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID      string          `json:"id"`
		Name    string          `json:"name"`
		Version int             `json:"version"`
		SavedAt time.Time       `json:"saved_at,omitzero"`
		Spec    panel.Dashboard `json:"spec"`
	}{r.ID, r.Name, r.Version, r.SavedAt, r.Spec})
}

// Summary lists a dashboard without its panels.
type Summary struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Panels  int       `json:"panels"`
	Version int       `json:"version"`
	SavedAt time.Time `json:"saved_at,omitzero"`
}

// Store keeps one file per dashboard under dir. A lock file serializes every
// process that writes, such as the app and driga mcp.
type Store struct {
	dir string
	now func() time.Time
}

// New returns the store in dataDir/dashboards.
func New(dataDir string) *Store {
	return &Store{dir: filepath.Join(dataDir, "dashboards"), now: time.Now}
}

type file struct {
	ID       string    `json:"id"`
	Versions []Version `json:"versions"`
}

func (s *Store) path(id string) string { return filepath.Join(s.dir, id+".json") }

func (s *Store) read(id string) (Record, error) {
	if !idPattern.MatchString(id) {
		return Record{}, ErrNotFound
	}
	b, err := os.ReadFile(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		if spec, ok := panel.Builtin(id); ok {
			return Record{ID: id, Name: spec.Name, Spec: spec}, nil
		}
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return Record{}, fmt.Errorf("read dashboard %s: %w", id, err)
	}
	if len(f.Versions) == 0 {
		return Record{}, ErrNotFound
	}
	last := f.Versions[len(f.Versions)-1]
	return Record{ID: id, Name: last.Spec.Name, Version: last.Version, SavedAt: last.SavedAt, Spec: last.Spec, history: f.Versions}, nil
}

// locked runs fn while holding the store's write lock.
func (s *Store) locked(fn func() error) error {
	if err := secfile.MkdirPrivate(s.dir); err != nil {
		return err
	}
	f, err := secfile.OpenFile(filepath.Join(s.dir, ".lock"), os.O_CREATE|os.O_RDWR)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := filelock.Lock(f, false); err != nil {
		return err
	}
	defer filelock.Unlock(f)
	return fn()
}

func (s *Store) save(r Record, spec panel.Dashboard, author Author, message string) (Record, error) {
	if err := panel.Validate(spec); err != nil {
		return Record{}, err
	}
	v := Version{Version: r.Version + 1, SavedAt: s.now().UTC(), Author: author, Message: strings.TrimSpace(message), Spec: spec}
	history := append(slices.Clone(r.history), v)
	if len(history) > maxVersions {
		history = history[len(history)-maxVersions:]
	}
	b, err := json.MarshalIndent(file{ID: r.ID, Versions: history}, "", "  ")
	if err != nil {
		return Record{}, err
	}
	if err := secfile.WriteFileAtomic(s.path(r.ID), b, nil); err != nil {
		return Record{}, err
	}
	return Record{ID: r.ID, Name: spec.Name, Version: v.Version, SavedAt: v.SavedAt, Spec: spec, history: history}, nil
}

// List returns the built-in dashboards, saved or not, then the user's own.
func (s *Store) List() ([]Summary, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	ids := slices.Clone(panel.BuiltinIDs)
	for _, e := range entries {
		if id, ok := strings.CutSuffix(e.Name(), ".json"); ok && idPattern.MatchString(id) && !slices.Contains(panel.BuiltinIDs, id) {
			ids = append(ids, id)
		}
	}
	out := make([]Summary, 0, len(ids))
	for _, id := range ids {
		r, err := s.read(id)
		if err != nil {
			continue
		}
		out = append(out, Summary{ID: r.ID, Name: r.Name, Panels: len(r.Spec.Panels), Version: r.Version, SavedAt: r.SavedAt})
	}
	return out, nil
}

// Get returns a dashboard's current spec.
func (s *Store) Get(id string) (Record, error) { return s.read(id) }

// Create saves a new dashboard and returns it as version 1.
func (s *Store) Create(spec panel.Dashboard, author Author, message string) (Record, error) {
	var out Record
	err := s.locked(func() error {
		id, err := s.newID(spec.Name)
		if err != nil {
			return err
		}
		out, err = s.save(Record{ID: id}, spec, author, message)
		return err
	})
	return out, err
}

func (s *Store) newID(name string) (string, error) {
	slug := strings.Trim(regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	if slug == "" {
		slug = "dashboard"
	}
	for range 8 {
		b := make([]byte, 3)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		id := slug + "-" + hex.EncodeToString(b)
		if _, err := os.Stat(s.path(id)); errors.Is(err, os.ErrNotExist) {
			return id, nil
		}
	}
	return "", errors.New("could not choose a dashboard id")
}

// update applies change to the current spec. base 0 means the latest; any
// other base must still be the current version.
func (s *Store) update(id string, base int, author Author, message string, change func(panel.Dashboard) (panel.Dashboard, error)) (Record, error) {
	var out Record
	err := s.locked(func() error {
		r, err := s.read(id)
		if err != nil {
			return err
		}
		if base != 0 && base != r.Version {
			return ErrStale
		}
		spec, err := change(clone(r.Spec))
		if err != nil {
			return err
		}
		out, err = s.save(r, spec, author, message)
		return err
	})
	return out, err
}

// Replace saves spec whole; panels it leaves out are removed.
func (s *Store) Replace(id string, spec panel.Dashboard, base int, author Author, message string) (Record, error) {
	return s.update(id, base, author, message, func(current panel.Dashboard) (panel.Dashboard, error) {
		keepGrids(current, &spec)
		return spec, nil
	})
}

// Edit applies ops in order, all or none.
func (s *Store) Edit(id string, ops []Operation, base int, author Author, message string) (Record, error) {
	return s.update(id, base, author, message, func(current panel.Dashboard) (panel.Dashboard, error) {
		return Apply(current, ops)
	})
}

// Versions lists a dashboard's saved versions, newest first.
func (s *Store) Versions(id string) ([]Version, error) {
	r, err := s.read(id)
	if err != nil {
		return nil, err
	}
	out := slices.Clone(r.history)
	slices.Reverse(out)
	return out, nil
}

// Restore saves an earlier version as the newest one.
func (s *Store) Restore(id string, version int, author Author) (Record, error) {
	return s.update(id, 0, author, fmt.Sprintf("Restored version %d", version), func(current panel.Dashboard) (panel.Dashboard, error) {
		r, err := s.read(id)
		if err != nil {
			return panel.Dashboard{}, err
		}
		if version == r.Version {
			return panel.Dashboard{}, ErrAlreadyLatest
		}
		for _, v := range r.history {
			if v.Version == version {
				return clone(v.Spec), nil
			}
		}
		return panel.Dashboard{}, ErrVersion
	})
}

func clone(d panel.Dashboard) panel.Dashboard {
	b, _ := json.Marshal(d)
	var out panel.Dashboard
	_ = json.Unmarshal(b, &out)
	return out
}

// keepGrids carries dragged positions over to a replacement that omits them.
func keepGrids(current panel.Dashboard, next *panel.Dashboard) {
	grids := map[string]*panel.Grid{}
	for _, p := range current.Panels {
		grids[p.ID] = p.Grid
	}
	for i := range next.Panels {
		if next.Panels[i].Grid == nil {
			next.Panels[i].Grid = grids[next.Panels[i].ID]
		}
	}
}
