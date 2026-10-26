package view

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

type boardReadCounter struct {
	Reader
	bulk, sessions int
}

func (r *boardReadCounter) BoardEvidence(since, until time.Time) ([]model.Event, map[int64][]store.ObservationMinute, error) {
	r.bulk++
	return r.Reader.BoardEvidence(since, until)
}
func (r *boardReadCounter) Sessions(f store.SessionFilter) ([]model.Session, error) {
	r.sessions++
	return r.Reader.Sessions(f)
}
func (*boardReadCounter) Events(string, string, time.Time) ([]model.Event, error) {
	panic("per-row event query")
}
func (*boardReadCounter) ObservationMinutes(int64, time.Time) ([]store.ObservationMinute, error) {
	panic("per-row observation query")
}

func TestLoadBoardSnapshotUsesBulkEvidence(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC()
	err = st.Tx(context.Background(), func(tx *store.Tx) error {
		for i := 0; i < 200; i++ {
			id, e := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: fmt.Sprint(i), Title: "Work", StartedAt: now.Add(-time.Hour), LastActivityAt: now}, Label: "working"})
			if e != nil {
				return e
			}
			if e = tx.Observe(id, now, 100, 20); e != nil {
				return e
			}
			if e = tx.InsertEvent(model.Event{Adapter: "claude", ExternalID: fmt.Sprint(i), TS: now, Kind: "otlp:tool_result"}); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	r := &boardReadCounter{Reader: st}
	snap := BoardSnapshot{Nodes: []FlowNode{}}
	err = loadBoardMachine(r, Filter{}, now, &snap)
	if err != nil {
		t.Fatal(err)
	}
	if r.bulk != 1 || r.sessions != 1 || len(snap.Nodes) != 200 {
		t.Fatal("not a bulk snapshot", r.bulk, r.sessions, len(snap.Nodes))
	}
	for _, n := range snap.Nodes {
		if len(n.Events) != 1 || n.Observed == nil {
			t.Fatal("lost bulk evidence")
		}
	}
}

func TestBoardOpenWaitUsesInboxLiveness(t *testing.T) {
	closed := testNow.Add(-time.Minute)
	for _, tc := range []struct {
		name, label                     string
		ended, acked, hosted, waitEnded bool
		wantLive                        bool
	}{
		{name: "idle notification", label: "idle"},
		{name: "working", label: "working"},
		{name: "live", label: "waiting", wantLive: true},
		{name: "ended session", label: "waiting", ended: true},
		{name: "acknowledged", label: "waiting", acked: true},
		{name: "closed history", label: "idle", waitEnded: true},
		{name: "hidden hosted session", label: "waiting", hosted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := fixture(t, func(tx *store.Tx) error {
				s := model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "notification", StartedAt: testNow.Add(-3 * time.Hour), LastActivityAt: testNow.Add(-2 * time.Hour), Hosted: tc.hosted}, Label: tc.label}
				if tc.ended {
					s.EndedAt = &closed
				}
				id, err := tx.UpsertSession(s)
				if err != nil {
					return err
				}
				item := model.InboxItem{ID: "notification", SessionID: id, Kind: "waiting", Reason: "idle_prompt", TS: testNow.Add(-2 * time.Minute)}
				if tc.acked {
					item.AckedAt = &closed
				}
				if tc.waitEnded {
					item.EndedAt = &closed
				}
				return tx.UpsertInboxItem(item)
			})
			inbox, err := Inbox(st, Filter{All: true}, testNow)
			if err != nil {
				t.Fatal(err)
			}
			if len(inbox.Rows) != 1 || inbox.Rows[0].Historical == tc.wantLive {
				t.Fatalf("inbox liveness: %+v", inbox)
			}
			snapshot, err := LoadBoardSnapshot(st, Filter{}, testNow)
			if err != nil {
				t.Fatal(err)
			}
			board := Board(snapshot, time.Hour, testNow)
			row := board.Groups[0].Rows[0]
			if (row.Wait != nil) != tc.wantLive {
				t.Fatalf("board wait disagrees with inbox: %+v", row.Wait)
			}
			if tc.label == "idle" || tc.label == "working" {
				if row.State != tc.label || row.WaitReason != "" || len(board.Needs) != 0 || board.Pulse["waiting"] != 0 {
					t.Fatalf("historical wait changed current state: %+v", row)
				}
			}
			if tc.wantLive && (row.State != "waiting" || row.WaitReason == "" || len(board.Needs) != 1 || board.Pulse["waiting"] != 1) {
				t.Fatalf("live wait lost: %+v", row)
			}
			if tc.waitEnded || tc.acked {
				if len(snapshot.Nodes[0].Waits) != 1 || snapshot.Nodes[0].Waits[0].EndedAt == nil {
					t.Fatal("closed timeline history lost")
				}
			}
			if tc.label == "idle" || tc.wantLive {
				agents, err := Agents(st, AgentFilter{}, testNow)
				if err != nil {
					t.Fatal(err)
				}
				if got := ProjectAttention(inbox, agents, "", nil).Header(OrphansView{}, SummaryView{}).NeedYou; got != len(board.Needs) {
					t.Fatalf("Need you=%d, board needs=%d", got, len(board.Needs))
				}
			}
		})
	}
}
