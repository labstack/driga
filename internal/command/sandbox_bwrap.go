package command

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func bubbleArgs(p Prepared) ([]string, error) {
	writes, err := sandboxWrites(p.Scope)
	if err != nil {
		return nil, err
	}
	// bubblewrap 0.9 validates --disable-userns before promoting the
	// --unshare-user-try implied by --unshare-all. Require it explicitly.
	a := []string{"--unshare-all", "--unshare-user", "--disable-userns", "--new-session", "--die-with-parent", "--cap-drop", "ALL"}
	if p.Parsed.Request.Network {
		a = append(a, "--share-net")
	}
	// Build a fresh root. Host /run, /tmp, home and pathname sockets are absent.
	for _, dir := range []string{"/usr", "/bin", "/sbin", "/lib", "/lib64"} {
		if _, e := os.Stat(dir); e == nil {
			a = append(a, "--ro-bind", dir, dir)
		}
	}
	a = append(a, "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp")
	for _, file := range []string{"/etc/resolv.conf", "/etc/hosts", "/etc/nsswitch.conf", "/etc/ld.so.cache"} {
		if _, e := os.Stat(file); e == nil {
			a = append(a, "--ro-bind", file, file)
		}
	}
	for _, root := range writes.Roots {
		if root != "" {
			a = append(a, "--bind", root, root)
		}
	}
	// Existing leaves are re-bound read-only after every writable bind. Reserve
	// the first absent ancestor too, so missing hooks/config cannot be planted.
	masked := []string{}
	dataFD := 4 // fd 3 carries seccomp; each data mount consumes its own fd.
	for _, entry := range writes.Protected {
		path := entry.Path
		covered := false
		for _, parent := range masked {
			if within(path, parent) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		st, err := os.Stat(path)
		if err == nil {
			if entry.Literal && st.IsDir() {
				a = append(a, "--bind", path, path)
				continue
			}
			a = append(a, "--ro-bind", path, path)
		} else if os.IsNotExist(err) {
			missing := path
			for parent := filepath.Dir(missing); ; parent = filepath.Dir(missing) {
				if _, err := os.Stat(parent); err == nil {
					break
				} else if !os.IsNotExist(err) {
					return nil, err
				}
				missing = parent
			}
			if missing == path && filepath.Base(path) == "commondir" {
				// An empty commondir breaks Git. A private descriptor supplies the
				// equivalent explicit self-reference for an ordinary repository.
				a = append(a, "--ro-bind-data", strconv.Itoa(dataFD), missing)
				dataFD++
			} else if entry.Literal || missing != path || strings.HasSuffix(path, "/.git") || strings.HasSuffix(path, "/hooks") || strings.HasSuffix(path, "/.vscode") || strings.HasSuffix(path, "/.idea") || strings.HasSuffix(path, "/.husky") {
				a = append(a, "--tmpfs", missing, "--remount-ro", missing)
			} else {
				a = append(a, "--ro-bind", "/dev/null", missing)
			}
			path = missing
		} else {
			return nil, err
		}
		masked = append(masked, path)
	}
	for _, root := range p.Scope.Denied {
		// Only roots made visible by a bind need masking; absent paths stay absent.
		visible := within(root, "/usr")
		for _, writeRoot := range writes.Roots {
			visible = visible || within(root, writeRoot)
		}
		if !visible {
			continue
		}
		if st, e := os.Stat(root); e == nil {
			if st.IsDir() {
				a = append(a, "--tmpfs", root, "--remount-ro", root)
			} else {
				a = append(a, "--ro-bind", "/dev/null", root)
			}
		}
	}
	return append(a, "--chdir", p.Parsed.Request.Cwd, "--seccomp", "3", "--"), nil
}

// bwrap otherwise creates missing bind destinations on the host itself. Own
// those harmless empty mountpoints and remove only the exact inodes we created.
func bubblePlaceholders(args []string) (func(), error) {
	type placeholder struct {
		path string
		info os.FileInfo
	}
	var created []placeholder
	cleanup := func() {
		for i := len(created) - 1; i >= 0; i-- {
			p := created[i]
			if st, err := os.Lstat(p.path); err == nil && os.SameFile(st, p.info) {
				_ = os.Remove(p.path)
			}
		}
	}
	for i := 0; i+1 < len(args); i++ {
		path, dir := "", false
		if args[i] == "--tmpfs" && args[i+1] != "/tmp" {
			path, dir = args[i+1], true
		}
		if i+2 < len(args) && args[i] == "--ro-bind" && args[i+1] == "/dev/null" {
			path = args[i+2]
		}
		common := args[i] == "--ro-bind-data" && i+2 < len(args)
		if common {
			path = args[i+2]
		}
		if path == "" {
			continue
		}
		if _, err := os.Lstat(path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			cleanup()
			return func() {}, err
		}
		var err error
		if dir {
			err = os.Mkdir(path, 0700)
		} else {
			var f *os.File
			f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err == nil {
				if common {
					_, err = f.WriteString(".\n")
				}
				closeErr := f.Close()
				if err == nil {
					err = closeErr
				}
			}
		}
		if err != nil {
			cleanup()
			return func() {}, err
		}
		st, err := os.Lstat(path)
		if err != nil {
			cleanup()
			return func() {}, err
		}
		created = append(created, placeholder{path, st})
	}
	return cleanup, nil
}
