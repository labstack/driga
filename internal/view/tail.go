package view

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

// TailEvent carries only stored text and a stable natural-key digest for reconnects.
type TailEvent struct {
	Key     string    `json:"key"`
	TS      time.Time `json:"ts"`
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
		for _, e := range events {
			key := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%s\x00%s", m.MachineID, e.Adapter, e.ExternalID, e.TS.UnixNano(), e.Kind, e.Summary))))
			state := "idle"
			switch strings.ToLower(e.Kind) {
			case "stop", "finished", "worker-finished", "sessionend":
				state = "finished"
			case "notification", "waiting", "permissionrequest":
				state = "waiting"
			case "stalled", "error":
				state = "stalled"
			case "sessionstart", "turn", "tool", "pretooluse", "posttooluse", "userpromptsubmit":
				state = "working"
			}
			rows = append(rows, TailEvent{Key: key, TS: e.TS, Machine: MachineName(m), Adapter: e.Adapter, Kind: e.Kind, Summary: e.Summary, State: state})
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
