package derive

import (
	"path"
	"strings"
)

// AppBundle returns the nearest enclosing .app directory of an absolute
// executable, shared by machine grouping and terminal click attribution.
func AppBundle(exe string) string {
	if !path.IsAbs(exe) {
		return ""
	}
	for dir := path.Dir(exe); dir != "/" && dir != "."; dir = path.Dir(dir) {
		if strings.HasSuffix(dir, ".app") {
			return dir
		}
	}
	return ""
}
