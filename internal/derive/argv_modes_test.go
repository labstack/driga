package derive

import (
	"strings"
	"testing"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
)

func TestClassifierArgvBoundaries(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		c := NewClassifier(signatures.Builtin(), 2)
		args := [][]string{{"codex", "-c", "label=true", "app-server"}, {"codex", "-c", "label=true app-server"}}
		wants := []signatures.Role{signatures.RoleHost, signatures.RoleInteractive}
		for j := range args {
			i := j
			if reverse {
				i = 1 - j
			}
			p := model.Proc{PID: 42, Cmdline: strings.Join(args[i], " "), Argv: args[i]}
			for range 2 {
				sig, ok := c.Match(p)
				if !ok || sig.Role != wants[i] {
					t.Errorf("argv %#v classified %q, want %q", p.Argv, sig.Role, wants[i])
				}
			}
		}
	}
}
