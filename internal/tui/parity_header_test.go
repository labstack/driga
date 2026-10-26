package tui

import (
	"context"
	"fmt"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/model"
	panels "github.com/labstack/driga/internal/panel"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
	"strings"
	"testing"
	"time"
)

func TestFlowIdleNotificationsAgreeWithAppPanels(t *testing.T) {
	for _, label := range []string{"idle", "waiting"} {
		t.Run(label, func(t *testing.T) {
			st, err := store.Open("", store.Memory)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { st.Close() })
			m := boardApp(t, "empty")
			now := m.opts.Now()
			err = st.Tx(context.Background(), func(tx *store.Tx) error {
				if err := tx.UpsertMachine(model.Machine{OS: "linux"}); err != nil {
					return err
				}
				for i := range 2 {
					id, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: fmt.Sprint(i), Title: "Finished turn", StartedAt: now.Add(-time.Hour), LastActivityAt: now.Add(-2 * time.Minute)}, Label: label})
					if err != nil {
						return err
					}
					for j := range 2 {
						if err := tx.UpsertInboxItem(model.InboxItem{ID: fmt.Sprintf("idle-prompt-%d-%d", i, j), SessionID: id, Kind: "waiting", Reason: "idle_prompt", TS: now.Add(-time.Minute)}); err != nil {
							return err
						}
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			src := NewStoreSource(st, t.TempDir(), view.Filter{})
			src.Now = func() time.Time { return now }
			m.snap, err = src.Load(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			m.width = 200
			m.rebuild()
			want := 0
			if label == "waiting" {
				want = 2
			}
			if len(m.board.Needs) != want || m.board.Pulse["waiting"] != want {
				t.Errorf("board needs=%d pulse=%d, want %d", len(m.board.Needs), m.board.Pulse["waiting"], want)
			}
			for _, g := range m.board.Groups {
				for _, row := range g.Rows {
					if row.State != label || label == "idle" && (row.Wait != nil || row.WaitReason != "") {
						t.Errorf("row state=%s wait=%+v reason=%q", row.State, row.Wait, row.WaitReason)
					}
				}
			}
			got := m.View().Content
			for _, text := range []string{fmt.Sprintf("Need you %d", want), fmt.Sprintf("%d waiting", want), fmt.Sprintf("NEEDS YOU · %d", want)} {
				if !strings.Contains(got, text) {
					t.Errorf("missing %q: %s", text, got)
				}
			}
			fragment, err := panels.Run(m.snap, panels.RunRequest{Dashboard: panels.Header()})
			if err != nil {
				t.Fatal(err)
			}
			if got := fragment.Results[0].Frame.Rows[0][0]; got != float64(want) {
				t.Errorf("app Need you=%v, want %d", got, want)
			}
			if len(fragment.WaitingSessions) != want {
				t.Errorf("app waiting sessions=%v, want %d", fragment.WaitingSessions, want)
			}
			fleet, err := panels.Run(m.snap, panels.RunRequest{Dashboard: panels.Dashboard{Name: "Sessions", Panels: []panels.Panel{{ID: "sessions", Title: "Sessions", Viz: "table", Source: "sessions", Columns: []string{"state"}}}}})
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range fleet.Results[0].Frame.Rows {
				if row[0] != label {
					t.Errorf("app session state=%v, want %s", row[0], label)
				}
			}
		})
	}
}

func TestHeaderShowsCPUAndDiskTotals(t *testing.T) {
	m := boardApp(t, "quiet")
	m.width = 250
	cpu := 37.0
	m.snap.Summary.Sample = model.MachineSample{TS: m.opts.Now(), CPUPercent: &cpu, MemUsed: 50, MemTotal: 100, DiskTotal: 500, DiskFree: 100, Load1: 9, Cores: 4}
	m.snap.Machine.Machines = nil
	got := m.header()
	if !strings.Contains(got, "CPU 37%") || strings.Contains(got, "load ") || !strings.Contains(got, "disk "+view.HumanBytes(400)+"/"+view.HumanBytes(500)) {
		t.Fatal(got)
	}
	m.snap.Summary.Sample.CPUPercent = nil
	m.snap.Summary.Sample.DiskTotal = 0
	got = m.header()
	if !strings.Contains(got, "load 9.0/4") || !strings.Contains(got, "disk free "+view.HumanBytes(100)) {
		t.Fatal(got)
	}
}

func TestFlowNeedYouMatchesBoardWaitingSessions(t *testing.T) {
	m := boardApp(t, "busy")
	m.width = 200
	m.snap.LocalMachineID = "local"
	m.snap.Agents.Rows = nil
	m.snap.Inbox.Rows = nil
	for _, r := range m.board.Needs {
		m.snap.Agents.Rows = append(m.snap.Agents.Rows, view.AgentRow{MachineID: r.Node.MachineID, ID: r.Node.ID, Label: "waiting"})
	}
	if len(m.board.Needs) == 0 {
		t.Fatal("fixture has no waiting sessions")
	}
	// A live approval for a session already waiting must not add another need.
	r := m.board.Needs[0]
	m.snap.LocalMachineID = r.Node.MachineID
	m.prompts = []chat.Prompt{{SessionID: r.Node.ID}}
	got, _ := m.boardLayout()
	if want := fmt.Sprintf("Need you %d", len(m.board.Needs)); !strings.Contains(got, want) {
		t.Fatalf("missing %q: %s", want, got)
	}
}
func TestFlowPulseUsesSharedAttentionAndHygiene(t *testing.T) {
	m := boardApp(t, "quiet")
	m.width = 200
	m.snap.LocalMachineID = "local"
	m.snap.Inbox.Rows = []view.InboxRow{{MachineID: "local", InboxItem: model.InboxItem{SessionID: 1, Kind: "waiting"}}, {MachineID: "local", InboxItem: model.InboxItem{SessionID: 2, Kind: "finished"}}, {MachineID: "local", InboxItem: model.InboxItem{SessionID: 2, Kind: "finished"}}}
	m.prompts = []chat.Prompt{{SessionID: 1}, {SessionID: 3}}
	m.snap.Agents.Rows = []view.AgentRow{{Label: "~stuck"}}
	m.snap.Orphans.Rows = []view.OrphanRow{{}}
	m.snap.Summary.Reclaim = 70
	m.snap.Summary.Sample.MemUsed = 50
	m.snap.Summary.Sample.MemTotal = 100
	got, _ := m.boardLayout()
	for _, want := range []string{"Need you 2", "To review 1", "Stuck 1", "Leftover 1", "Reclaimable " + view.HumanBytes(70), "Memory " + view.HumanBytes(50) + "/" + view.HumanBytes(100)} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
}
