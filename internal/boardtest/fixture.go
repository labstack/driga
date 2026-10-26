package boardtest

import (
	"fmt"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
	"time"
)

// BoardFixture is entirely synthetic and never launches or inspects an agent.
// Busy has seventy sessions across twelve projects, two machines and families.
func BoardFixture(kind string, now time.Time) view.BoardSnapshot {
	count := 70
	if kind == "quiet" {
		count = 2
	}
	if kind == "empty" {
		count = 0
	}
	b := view.BoardSnapshot{Remote: map[string]bool{"studio-mac": true}, Files: map[string][]model.FileTouch{}, Nodes: []view.FlowNode{}, Load: []int{1, 3, 2, 4, 7, 3, 2, 3, 6, 8, 4, 5}, Memory: "mem 82%"}
	names := []string{"driga", "echo-pro", "monk", "inloop", "canon", "atlas", "studio", "orbit", "relay", "小さな道具", "northstar", "platform"}
	titles := []string{"Add board view", "Fix retry after reconnect", "Permission boundary audit", "Polish onboarding copy", "流れを整える 🌊", "Investigate cache invalidation", "Ship the release pipeline safely", "An intentionally long title with CJK 東京 and emoji 🛰️ — verify alignment at every width"}
	for i := 0; i < count; i++ {
		state := []string{"working", "working", "waiting", "stuck", "working", "idle", "working", "finished", "working", "idle"}[i%10]
		if kind == "quiet" {
			state = "idle"
		}
		id := int64(i + 1)
		adapter := []string{"claude", "codex", "copilot", "gemini"}[i%4]
		machine := "local"
		if i%5 == 4 {
			machine = "studio-mac"
		}
		n := view.FlowNode{ID: id, MachineID: machine, ExternalID: fmt.Sprint("synthetic-", id), Adapter: adapter, Title: titles[i%len(titles)], Project: names[i%12], Branch: "main", Cwd: "/synthetic/" + names[i%12], StartedAt: now.Add(-time.Duration(45+i) * time.Minute), LastActivityAt: now.Add(-time.Duration(i%12) * time.Minute), Label: state, Mixer: model.StateMixer{Verdict: state, Evidence: "telemetry: tool_result", ObservedAt: now.Add(-2 * time.Second)}, Events: []model.Event{}, Waits: []view.FlowWait{}}
		if i%8 == 0 {
			n.StartedFrom = "Editor"
			n.Branch = "feat/board"
			n.Cwd += "-worktree"
		}
		if i >= 48 && i < 60 {
			p := int64(i - 47)
			n.ParentID = &p
			n.Project = names[(i-48)%12]
			n.MachineID = "local"
		}
		if state == "waiting" {
			n.Waits = []view.FlowWait{{ID: fmt.Sprint("wait-", id), StartedAt: now.Add(-time.Duration(3+i%21) * time.Minute), Reason: "Permission · Bash", Detail: "go test ./..."}}
			n.Mixer.Evidence = "open permission prompt"
		}
		if state == "stuck" {
			n.Mixer.Evidence = "no transcript growth for 18m"
		}
		if state == "finished" {
			end := now.Add(-time.Duration(4+i%25) * time.Minute)
			n.EndedAt = &end
		}
		if state != "idle" {
			o := &view.FlowObserved{FromAt: now.Truncate(time.Minute).Add(-59 * time.Minute), LogBytesPerMin: make([]int64, 60), CPUMSPerMin: make([]int64, 60)}
			for c := range o.LogBytesPerMin {
				if c%13 < 8 && c > i%20 {
					o.LogBytesPerMin[c] = int64((c+i)%7+1) * 100
					o.CPUMSPerMin[c] = 30
				}
			}
			n.Observed = o
			cost := float64(i%7+1) * .07
			in, out := int64(120000+i*200), int64(8400+i*17)
			n.Events = []model.Event{{Adapter: adapter, TS: now.Add(-6 * time.Minute), Kind: "otlp:api_request", Metadata: &model.EventMetadata{InputTokens: &in, OutputTokens: &out, CostUSD: &cost}}, {Adapter: adapter, TS: now.Add(-2 * time.Minute), Kind: "otlp:tool_result", Summary: "go test ./...", Metadata: &model.EventMetadata{Tool: "Bash"}}}
		}
		if i == 0 || i == 12 {
			b.Files[view.BoardKey(n)] = []model.FileTouch{{Path: "/synthetic/driga/board.go", TS: now.Add(-time.Minute)}}
		}
		b.Nodes = append(b.Nodes, n)
	}
	return b
}
