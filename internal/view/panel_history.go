package view

import (
	"fmt"
	"github.com/labstack/driga/internal/model"
	"time"
)

// AttentionPoint is one minute's attention, derived from durable inbox intervals.
// Stuck is unknown until the first stored poll: no pre-install history is invented.
type AttentionPoint struct {
	TS      time.Time `json:"ts"`
	Waiting int       `json:"waiting"`
	Review  int       `json:"review"`
	Stuck   *int      `json:"stuck"`
}
type MachinePoll struct {
	MachineID string    `json:"machine_id"`
	TS        time.Time `json:"ts"`
	State     string    `json:"state"`
}
type PanelHistoryView struct {
	Attention []AttentionPoint `json:"attention"`
	Polls     []MachinePoll    `json:"polls"`
}

// AttentionAt reconstructs unread completions (one per session), waits and
// active machine warnings at a historical instant, including later acknowledgments.
func AttentionAt(items []model.InboxItem, at time.Time) (waiting, review int) {
	finished := map[string]bool{}
	for _, i := range items {
		if i.TS.After(at) || i.AckedAt != nil && !i.AckedAt.After(at) || i.EndedAt != nil && !i.EndedAt.After(at) {
			continue
		}
		switch i.Kind {
		case "finished":
			if !i.TS.Before(at.Add(-24 * time.Hour)) {
				key := fmt.Sprintf("%s/%d", i.MachineID, i.SessionID)
				if i.SessionID == 0 {
					key = i.MachineID + "/item/" + i.ID
				}
				finished[key] = true
			}
		case "waiting":
			waiting++
		case "worker_finished":
		default:
			waiting++
		}
	}
	return waiting, len(finished)
}

// PanelHistory reads all selected machines in the caller's snapshot transaction.
func PanelHistory(r Reader, f Filter, now time.Time) (PanelHistoryView, error) {
	v := PanelHistoryView{Attention: make([]AttentionPoint, 60)}
	unknownStuck := make([]bool, 60)
	start := now.UTC().Truncate(time.Minute).Add(-59 * time.Minute)
	for i := range v.Attention {
		v.Attention[i].TS = start.Add(time.Duration(i) * time.Minute)
	}
	err := machineReaders(r, f.Scope, func(mr Reader, _ model.Machine) error {
		items, err := mr.Inbox(time.Time{}, true)
		if err != nil {
			return err
		}
		prompts, err := mr.PromptIntervals(start)
		if err != nil {
			return err
		}
		polls, err := mr.PanelPolls(now)
		if err != nil {
			return err
		}
		for i := range v.Attention {
			p := &v.Attention[i]
			// The current minute uses now, so the latest count agrees with the header.
			at := p.TS
			if i == 59 {
				at = now
			}
			w, review := AttentionAt(items, at)
			p.Waiting += w
			for _, prompt := range prompts {
				if !prompt.StartedAt.After(at) && (prompt.EndedAt == nil || prompt.EndedAt.After(at)) {
					duplicate := false
					for _, item := range items {
						if prompt.SessionID > 0 && item.SessionID == prompt.SessionID && item.Kind == "waiting" && !item.TS.After(at) && (item.EndedAt == nil || item.EndedAt.After(at)) && (item.AckedAt == nil || item.AckedAt.After(at)) {
							duplicate = true
							break
						}
					}
					if !duplicate {
						p.Waiting++
					}
				}
			}
			p.Review += review
			j := -1
			for n := range polls {
				if !polls[n].TS.After(at) {
					j = n
				}
			}
			if j < 0 || at.Sub(polls[j].TS) >= time.Minute {
				unknownStuck[i] = true
			}
			if j >= 0 && at.Sub(polls[j].TS) < time.Minute {
				if p.Stuck == nil {
					p.Stuck = new(int)
				}
				*p.Stuck += polls[j].Stuck
			}
		}
		if len(polls) > 30 {
			polls = polls[len(polls)-30:]
		}
		for _, p := range polls {
			v.Polls = append(v.Polls, MachinePoll{mr.MachineID(), p.TS, p.Worst})
		}
		return nil
	})
	for i, unknown := range unknownStuck {
		if unknown {
			v.Attention[i].Stuck = nil
		}
	}
	return v, err
}
