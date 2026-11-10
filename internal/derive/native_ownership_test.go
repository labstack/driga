package derive

import (
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
)

func TestNativeAgentOwnershipUsesNativeOwnerAndTopology(t *testing.T) {
	for _, sourceOS := range []string{"linux", "windows"} {
		for _, scenario := range []string{"overlap", "reverse overlap", "ancestor", "contained", "unknown owner", "missing cwd", "foreign owner", "disjoint", "host", "headless"} {
			t.Run(sourceOS+"/"+scenario, func(t *testing.T) {
				start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
				self := model.Proc{PID: 900, OwnerKnown: true, OwnerUID: 123, OwnerSID: "S-1-5-21-123"}
				root := model.Proc{PID: 10, PPID: 1, StartTime: start, Cwd: "/work/project/nested"}
				agent := model.Proc{PID: 90, PPID: 2, StartTime: start, OwnerKnown: true, OwnerUID: self.OwnerUID, OwnerSID: self.OwnerSID, Exe: "/tools/codex", Cmdline: "codex", Argv: []string{"codex"}, Cwd: "/work/project"}
				switch scenario {
				case "reverse overlap":
					root.Cwd, agent.Cwd = agent.Cwd, root.Cwd
				case "ancestor":
					root.PPID = agent.PID
					agent.Cwd = "/unrelated"
				case "contained":
					agent.PPID = root.PID
					agent.Cwd = "/unrelated"
				case "unknown owner":
					agent.OwnerKnown = false
				case "missing cwd":
					agent.Cwd = ""
				case "foreign owner":
					agent.OwnerUID, agent.OwnerSID = 124, "S-1-5-21-124"
				case "disjoint":
					agent.Cwd = "/work/project-two"
				case "host":
					agent.Cmdline, agent.Argv = "codex app-server", []string{"codex", "app-server"}
				case "headless":
					agent.Cmdline, agent.Argv = "codex exec task", []string{"codex", "exec", "task"}
				}
				if sourceOS == "windows" {
					root.Cwd = "C:" + strings.ReplaceAll(root.Cwd, "/", `\`)
					if agent.Cwd != "" {
						agent.Cwd = "c:" + strings.ReplaceAll(agent.Cwd, "/", `\`)
					}
				}
				procs := []model.Proc{root, agent}
				idx, err := NewProcIndexForOS(procs, sourceOS)
				if err != nil {
					t.Fatal(err)
				}
				agents := NativeAgentProcesses(procs, self, sourceOS, NewClassifier(signatures.Builtin(), len(procs)))
				reason := NativeAgentOwnershipReason(idx, append([]model.Proc{root}, idx.Descendants(root.PID)...), agents, self, sourceOS)
				want := scenario != "foreign owner" && scenario != "disjoint" && scenario != "host"
				if (reason != "") != want {
					t.Fatalf("ownership reason=%q, want refusal=%t", reason, want)
				}
			})
		}
	}
}
