package cleanup

import (
	"iter"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/pathx"
)

// These indexes belong to one fresh process/store observation, never a listed
// candidate or Env. Verification builds new indexes, including new symlink
// resolutions. Keep the original session cwd for scratch-directory attribution.
type snapshotPaths struct {
	cwds, open []string
	sessions   []sessionPath
}

type sessionPath struct {
	key     string
	session *model.Session
}

func (s *snapshot) preparePaths() {
	p := &snapshotPaths{}
	canonical := make(map[string]string)
	key := func(path string) string {
		if !filepath.IsAbs(path) {
			return ""
		}
		if cached, ok := canonical[path]; ok {
			return cached
		}
		v := pathx.Key(resolved(path))
		canonical[path] = v
		return v
	}
	for _, path := range s.cwds {
		if v := key(path); v != "" {
			p.cwds = append(p.cwds, v)
		}
	}
	for _, path := range s.open {
		if v := key(path); v != "" {
			// Preserve the native spelling as well as its canonical target: an
			// alias must never weaken an existing open-file refusal.
			p.open = append(p.open, pathx.Key(path), v)
		}
	}
	for i := range s.sessions {
		if v := key(s.sessions[i].Cwd); v != "" {
			p.sessions = append(p.sessions, sessionPath{v, &s.sessions[i]})
		}
	}
	slices.Sort(p.cwds)
	p.cwds = slices.Compact(p.cwds)
	slices.Sort(p.open)
	p.open = slices.Compact(p.open)
	slices.SortFunc(p.sessions, func(a, b sessionPath) int { return strings.Compare(a.key, b.key) })
	s.paths = p
}

func (s snapshot) pathEvidence() *snapshotPaths {
	// Direct inspector tests can supply a raw snapshot; production read prepares
	// it once before any candidate inspection, without retaining a mutable cache.
	if s.paths == nil {
		s.preparePaths()
	}
	return s.paths
}

func observedUnder(paths []string, root string) bool {
	if !filepath.IsAbs(root) {
		return false
	}
	key := pathx.Key(root)
	if _, found := slices.BinarySearch(paths, key); found {
		return true
	}
	prefix := strings.TrimSuffix(key, "/") + "/"
	i, _ := slices.BinarySearch(paths, prefix)
	return i < len(paths) && strings.HasPrefix(paths[i], prefix)
}

func (s snapshot) cwdUnder(root string) bool {
	return observedUnder(s.pathEvidence().cwds, root)
}

func (s snapshot) sessionsUnder(root string) iter.Seq[model.Session] {
	return func(yield func(model.Session) bool) {
		if !filepath.IsAbs(root) {
			return
		}
		paths := s.pathEvidence().sessions
		key := pathx.Key(root)
		first := sort.Search(len(paths), func(i int) bool { return paths[i].key >= key })
		for i := first; i < len(paths) && paths[i].key == key; i++ {
			if !yield(*paths[i].session) {
				return
			}
		}
		prefix := strings.TrimSuffix(key, "/") + "/"
		first = sort.Search(len(paths), func(i int) bool { return paths[i].key >= prefix })
		for i := first; i < len(paths) && strings.HasPrefix(paths[i].key, prefix); i++ {
			if paths[i].key != key && !yield(*paths[i].session) {
				return
			}
		}
	}
}
