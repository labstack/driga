// Package pathx defines lexical host path identity and containment. It does not
// resolve symlinks; callers requiring filesystem identity must resolve first.
package pathx

import (
	"path"
	"runtime"
	"strings"
)

func Within(p, root string) bool { return withinForOS(p, root, runtime.GOOS) }
func Equal(a, b string) bool     { return Key(a) == Key(b) }
func Key(p string) string        { return keyForOS(p, runtime.GOOS) }

func keyForOS(p, goos string) string {
	if p == "" {
		return ""
	}
	if goos != "windows" {
		return path.Clean(p)
	}
	p = strings.ReplaceAll(p, `\`, "/")
	if strings.HasPrefix(p, "//?/") {
		p = p[4:]
		if len(p) >= 4 && strings.EqualFold(p[:4], "UNC/") {
			p = "//" + p[4:]
		}
	}
	p = strings.ToLower(p)
	// Preserve the UNC server/share as a volume, so .. cannot leave it.
	if strings.HasPrefix(p, "//") {
		parts := strings.SplitN(p[2:], "/", 3)
		if len(parts) >= 2 {
			volume := "//" + parts[0] + "/" + parts[1]
			if len(parts) == 2 {
				return volume + "/"
			}
			return volume + path.Clean("/"+parts[2])
		}
	}
	if len(p) >= 2 && p[1] == ':' {
		if len(p) > 2 && p[2] == '/' {
			return p[:2] + path.Clean(p[2:])
		}
		return p[:2] + strings.TrimPrefix(path.Clean("./"+p[2:]), "./")
	}
	return path.Clean(p)
}

func withinForOS(p, root, goos string) bool {
	if p == "" || root == "" {
		return false
	}
	if goos == "windows" {
		p, root = keyForOS(p, goos), keyForOS(root, goos)
		if root == "." {
			return !strings.HasPrefix(p, "/") && !(len(p) > 1 && p[1] == ':') && p != ".." && !strings.HasPrefix(p, "../")
		}
		return p == root || strings.HasPrefix(p, strings.TrimSuffix(root, "/")+"/")
	}
	p, root = path.Clean(p), path.Clean(root)
	if path.IsAbs(p) != path.IsAbs(root) {
		return false
	}
	if root == "." {
		return p != ".." && !strings.HasPrefix(p, "../")
	}
	return p == root || strings.HasPrefix(p, strings.TrimSuffix(root, "/")+"/")
}
