package view

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

// TailEvent carries only stored text and a stable natural-key digest for reconnects.
type TailEvent struct {
	Key     string    `json:"key"`
	TS      time.Time `json:"occurred_at"`
	Machine string    `json:"machine"`
	Adapter string    `json:"adapter"`
	Kind    string    `json:"kind"`
	Summary string    `json:"summary"`
	State   string    `json:"state"`
}

// EventTail merges bounded machine reads into an oldest-first tail of 200 lines.
func EventTail(r Reader, scope store.Scope, now time.Time) ([]TailEvent, error) {
	rows := []TailEvent{}
	err := machineReaders(r, scope, func(mr Reader, m model.Machine) error {
		events, err := mr.RecentEvents(now, 200)
		if err != nil {
			return err
		}
		sessions, err := mr.Sessions(store.SessionFilter{EndedSince: now.Add(-24 * time.Hour)})
		if err != nil {
			return err
		}
		repos, err := mr.Repos()
		if err != nil {
			return err
		}
		context := map[[2]string]string{}
		for _, session := range sessions {
			title := session.Title
			if title == "" {
				title = session.LastPrompt
			}
			if title == "" {
				title = "session " + session.ExternalID
			}
			project := projectName(session, repos)
			if project == "" && session.Cwd != "" {
				project = filepath.Base(session.Cwd)
			}
			if project != "" {
				title = project + " · " + title
			}
			context[[2]string{session.Adapter, session.ExternalID}] = title
		}
		for _, e := range events {
			description := context[[2]string{e.Adapter, e.ExternalID}]
			if description == "" {
				description = "session " + e.ExternalID
			}
			if e.Summary != "" {
				description += " · " + e.Summary
			}
			key := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%s\x00%s", m.MachineID, e.Adapter, e.ExternalID, e.TS.UnixNano(), e.Kind, e.Summary))))
			state := "idle"
			switch strings.ToLower(e.Kind) {
			case "stop", "finished", "worker_finished", "sessionend":
				state = "finished"
			case "notification", "waiting", "permissionrequest":
				state = "waiting"
			case "stalled", "error":
				state = "stalled"
			case "sessionstart", "turn", "tool", "pretooluse", "posttooluse", "userpromptsubmit":
				state = "working"
			}
			rows = append(rows, TailEvent{Key: key, TS: e.TS, Machine: MachineName(m), Adapter: e.Adapter, Kind: e.Kind, Summary: description, State: state})
		}
		return nil
	})
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].TS.Equal(rows[j].TS) {
			return rows[i].Key < rows[j].Key
		}
		return rows[i].TS.Before(rows[j].TS)
	})
	if len(rows) > 200 {
		rows = rows[len(rows)-200:]
	}
	return rows, err
}
