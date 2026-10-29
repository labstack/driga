package command

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Literal directories pin metadata roots without making ordinary Git state
// read-only. All other entries deny writes to the path and its descendants.
type protectedWrite struct {
	Path    string
	Literal bool
}

type writePolicy struct {
	Roots     []string
	Protected []protectedWrite
}

// Read Git's indirection files ourselves: running git here could execute
// repository configuration before the sandbox exists.
func sandboxWrites(s Scope) (writePolicy, error) {
	w := writePolicy{}
	var err error
	if s.Project != "" {
		s.Project, err = canonical(s.Project)
		if err != nil {
			return w, err
		}
	}
	if s.Scratch != "" {
		s.Scratch, err = canonical(s.Scratch)
		if err != nil {
			return w, err
		}
	}
	seenRoots, seenGit := map[string]bool{}, map[string]bool{}
	protected := map[string]bool{}
	add := func(path string, literal bool) error {
		// A file bind follows a symlink, leaving the link itself replaceable.
		// Refuse that layout, including symlink ancestors such as .git/info.
		for p := path; !seenRoots[p] && p != filepath.Dir(p); p = filepath.Dir(p) {
			st, err := os.Lstat(p)
			if err == nil && st.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("protected write path is a symlink: %s", p)
			}
			if err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		q, err := canonical(path)
		if err != nil {
			return err
		}
		for _, p := range []string{filepath.Clean(path), q} {
			if old, ok := protected[p]; !ok || old {
				protected[p] = literal
			}
		}
		return nil
	}
	var addRoot func(string) error
	addRoot = func(root string) error {
		root, err := canonical(root)
		if err != nil {
			return err
		}
		if seenRoots[root] {
			return nil
		}
		home, _ := os.UserHomeDir()
		if root == filepath.Dir(root) || root == home || denied(root, s) {
			return fmt.Errorf("unsafe sandbox write root: %s", root)
		}
		seenRoots[root] = true
		w.Roots = append(w.Roots, root)
		for _, name := range []string{".gitattributes", ".envrc", ".vscode", ".idea", ".husky"} {
			if err := add(filepath.Join(root, name), false); err != nil {
				return err
			}
		}
		return nil
	}
	var gitDir func(string, bool) error
	gitDir = func(dir string, external bool) error {
		dir, err := canonical(dir)
		if err != nil {
			return err
		}
		if seenGit[dir] {
			return nil
		}
		if !external && !slices.ContainsFunc(w.Roots, func(root string) bool { return within(dir, root) }) {
			return fmt.Errorf("nested gitdir cannot expand sandbox write scope: %s", dir)
		}
		if !within(dir, s.Project) && !within(dir, s.Scratch) {
			// Pointers must not turn arbitrary host directories into write roots.
			// External metadata has HEAD and either commondir or objects/refs.
			head, err := os.Lstat(filepath.Join(dir, "HEAD"))
			if err != nil || !head.Mode().IsRegular() {
				return fmt.Errorf("external gitdir has no regular HEAD: %s", dir)
			}
			if _, err := os.Lstat(filepath.Join(dir, "commondir")); os.IsNotExist(err) {
				for _, name := range []string{"objects", "refs"} {
					st, err := os.Lstat(filepath.Join(dir, name))
					if err != nil || !st.IsDir() {
						return fmt.Errorf("external gitdir has no %s directory: %s", name, dir)
					}
				}
			} else if err != nil {
				return err
			}
		}
		seenGit[dir] = true
		if err := addRoot(dir); err != nil {
			return err
		}
		if err := add(dir, true); err != nil {
			return err
		}
		if err := add(filepath.Join(dir, "info"), true); err != nil {
			return err
		}
		for _, name := range []string{"hooks", "config", "config.worktree", "info/attributes", "commondir", "gitdir"} {
			if err := add(filepath.Join(dir, name), false); err != nil {
				return err
			}
		}
		common, err := os.ReadFile(filepath.Join(dir, "commondir"))
		if err == nil {
			path := strings.TrimSpace(string(common))
			if !filepath.IsAbs(path) {
				path = filepath.Join(dir, path)
			}
			if err := gitDir(path, external); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		entries, err := os.ReadDir(filepath.Join(dir, "worktrees"))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		for _, e := range entries {
			if e.IsDir() {
				if err := gitDir(filepath.Join(dir, "worktrees", e.Name()), external); err != nil {
					return err
				}
			}
		}
		return nil
	}
	repo := func(root string) error {
		if err := addRoot(root); err != nil {
			return err
		}
		if err := add(root, true); err != nil {
			return err
		}
		path := filepath.Join(root, ".git")
		st, err := os.Lstat(path)
		if os.IsNotExist(err) {
			// Reserve the entire absent gitdir, including future hooks/config.
			return add(path, false)
		}
		if err != nil {
			return err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("protected gitdir is a symlink: %s", path)
		}
		external := root == s.Project || root == s.Scratch
		if st.IsDir() {
			return gitDir(path, external)
		}
		if err := add(path, false); err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		value, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
		if !ok {
			return fmt.Errorf("invalid gitdir file: %s", path)
		}
		value = strings.TrimSpace(value)
		if !filepath.IsAbs(value) {
			value = filepath.Join(root, value)
		}
		return gitDir(value, external)
	}
	for _, root := range []string{s.Project, s.Scratch} {
		if root == "" {
			continue
		}
		if err := repo(root); err != nil {
			return w, err
		}
		// Include existing nested repositories, without walking objects or refs.
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if path != root && (denied(path, s) || d.Name() == ".git") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.IsDir() || path == root {
				return nil
			}
			if _, err := os.Lstat(filepath.Join(path, ".git")); err == nil {
				return repo(path)
			} else if !os.IsNotExist(err) {
				return err
			}
			return nil
		})
		if err != nil {
			return w, err
		}
	}
	slices.Sort(w.Roots)
	// Parent binds already include their descendants. Keep only the effective
	// roots so cards disclose external common directories without redundant binds.
	roots := w.Roots[:0]
	for _, root := range w.Roots {
		covered := false
		for _, parent := range roots {
			if within(root, parent) {
				covered = true
				break
			}
		}
		if !covered {
			roots = append(roots, root)
		}
	}
	w.Roots = roots
	for path, literal := range protected {
		w.Protected = append(w.Protected, protectedWrite{path, literal})
	}
	slices.SortFunc(w.Protected, func(a, b protectedWrite) int { return strings.Compare(a.Path, b.Path) })
	return w, nil
}
