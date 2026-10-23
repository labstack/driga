package cli

import (
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestCopilotParentMustMatchSignature(t *testing.T) {
	for _, tc := range []struct {
		exe, cmd string
		want     bool
	}{
		{"/usr/bin/zsh", "zsh -c driga hook copilot sessionStart", false},
		{"/opt/copilot", "unusual argv", true},
		{"/usr/bin/node", "node /opt/node_modules/@github/copilot/npm-loader.js", true},
		{"/usr/bin/node", "node /other/app.js", false},
	} {
		t.Run(tc.cmd, func(t *testing.T) {
			hookTestEnv(t)
			old := hookParentProcess
			t.Cleanup(func() { hookParentProcess = old })
			hookParentProcess = func() (model.Proc, error) {
				return model.Proc{PID: 123, StartTime: time.Unix(50, 0), Exe: tc.exe, Cmdline: tc.cmd}, nil
			}
			pid, start, err := hookParentIdentity()
			if err != nil || (pid != 0) != tc.want || (!tc.want && !start.IsZero()) {
				t.Fatal(tc, pid, start, err)
			}
		})
	}
}
