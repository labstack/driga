package view

import (
	"fmt"
	"slices"
	"time"

	"github.com/labstack/driga/internal/attention"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

// LoadBoardSnapshot reads overview evidence once per machine, in the caller's
// snapshot transaction. No family or session detail builders are invoked.
func LoadBoardSnapshot(r Reader, f Filter, now time.Time) (BoardSnapshot, error) {
	b := BoardSnapshot{Nodes: []FlowNode{}, Load: []int{}, Files: map[string][]model.FileTouch{}, Remote: map[string]bool{}}
	err := machineReaders(r, f.Scope, func(mr Reader, _ model.Machine) error {
		b.Remote[mr.MachineID()] = mr.MachineID() != r.MachineID()
		return loadBoardMachine(mr, f, now, &b)
	})
	slices.SortFunc(b.History, func(x, y HistoryRow) int { return y.EndedAt.Compare(x.EndedAt) })
	if len(b.History) > HistoryLimit {
		b.History = b.History[:HistoryLimit]
	}
	return b, err
}

// sessionTitle is what the board calls a session: its title, else its last
// prompt, else a neutral name when the capture policy keeps no text.
func sessionTitle(s model.Session, capture string) string {
	title := s.Title
	if title == "" {
		title = s.LastPrompt
	}
	if capture == "none" || title == "" {
		title = flowAttentionTitle(s, "", capture)
	}
	return title
}

func loadBoardMachine(mr Reader, f Filter, now time.Time, b *BoardSnapshot) error {
	sessions, err := mr.Sessions(store.SessionFilter{IncludeEnded: true, RepoPath: f.Project})
	if err != nil {
		return err
	}
	repos, err := mr.Repos()
	if err != nil {
		return err
	}
	inbox, err := mr.Inbox(time.Time{}, true)
	if err != nil {
		return err
	}
	events, observed, err := mr.BoardEvidence(now.Add(-time.Hour), now)
	if err != nil {
		return err
	}
	byEvent := map[string][]model.Event{}
	for _, e := range events {
		key := e.Adapter + ":" + e.ExternalID
		byEvent[key] = append(byEvent[key], e)
	}
	byWait := map[int64][]FlowWait{}
	jumps := map[int64]*model.InboxJump{}
	byID := make(map[int64]model.Session, len(sessions))
	for _, s := range sessions {
		byID[s.ID] = s
	}
	for _, item := range inbox {
		if item.Kind != "waiting" {
			continue
		}
		item = attention.Policy(item, f.CaptureText)
		wait := flowWait(item)
		s, known := byID[item.SessionID]
		live := liveSessionWait(item, s, known, now)
		// Closed spans remain timeline history; only live open waits can
		// change a row's current state or supply its wait reason and jump.
		if wait.EndedAt == nil && !live {
			continue
		}
		byWait[item.SessionID] = append(byWait[item.SessionID], wait)
		if live {
			jumps[item.SessionID] = item.Jump
		}
	}
	files, err := mr.SessionFiles(now.Add(-time.Hour))
	if err != nil {
		return err
	}
	if b.Files == nil {
		b.Files = map[string][]model.FileTouch{}
	}
	for id, touches := range files {
		b.Files[BoardKey(FlowNode{MachineID: mr.MachineID(), ID: id})] = touches
	}
	for _, s := range sessions {
		if s.EndedAt != nil && s.ParentID == nil && s.EndedAt.After(now.Add(-HistoryWindow)) {
			h := HistoryRow{MachineID: mr.MachineID(), ID: s.ID, Title: sessionTitle(s, f.CaptureText), Project: projectName(s, repos), Branch: s.Branch, Adapter: s.Adapter, StartedAt: s.StartedAt, EndedAt: *s.EndedAt}
			if j := attention.ResolveJump(s, "", nil); j != nil {
				h.Resume = "cd " + shellQuote(s.Cwd) + " && " + j.Command
			}
			b.History = append(b.History, h)
		}
		if s.EndedAt != nil && s.EndedAt.Before(now.Add(-time.Hour)) {
			continue
		}
		n := FlowNode{Cwd: s.Cwd, StartedFrom: s.StartedFrom, MachineID: mr.MachineID(), ID: s.ID, ExternalID: s.ExternalID, ParentID: s.ParentID, StartedAt: s.StartedAt, EndedAt: s.EndedAt, Adapter: s.Adapter, Title: sessionTitle(s, f.CaptureText), Project: projectName(s, repos), Branch: s.Branch, Label: s.Label, Mixer: derive.SessionMixer(s), LastActivityAt: s.LastActivityAt, Events: byEvent[s.Adapter+":"+s.ExternalID], Waits: byWait[s.ID], Observed: flowObserved(observed[s.ID], now, 240), Jump: attention.ResolveJump(s, "", nil)}
		if jumps[s.ID] != nil {
			n.Jump = jumps[s.ID]
		}
		b.Nodes = append(b.Nodes, n)
	}
	samples, err := mr.Samples(now.Add(-time.Hour))
	if err != nil {
		return err
	}
	for _, s := range samples {
		b.Load = append(b.Load, int(s.Load1*10))
	}
	if len(b.Load) > 60 {
		b.Load = b.Load[len(b.Load)-60:]
	}
	if sample, err := mr.LatestSample(); err == nil && sample.MemTotal > 0 {
		b.Memory = fmt.Sprintf("mem %.0f%%", float64(sample.MemUsed)*100/float64(sample.MemTotal))
	}
	return nil
}
