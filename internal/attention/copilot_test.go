package attention

import (
	"testing"

	"github.com/labstack/driga/internal/model"
)

func TestCopilotResumeAndTTYJump(t *testing.T) {
	s := model.Session{SessionObs: model.SessionObs{Adapter: "copilot", ExternalID: "session'quoted", Cwd: "/repo"}}
	j := ResolveJump(s, "", nil)
	if j == nil || j.Kind != "resume" || j.Command != "copilot --resume="+Quote(s.ExternalID) {
		t.Fatal(j)
	}
	j = ResolveJump(s, "/dev/pts/1", []Pane{{TTY: "/dev/pts/1", Target: "work:1.1"}})
	if j == nil || j.Kind != "tmux" || j.Target != "work:1.1" {
		t.Fatal(j)
	}
}
