package store

import (
	"context"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestLabelInputsKeepLocalUnseenSemantics(t *testing.T) {
	st, err := Open("", Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Now().UTC()
	var one, two int64
	err = st.Tx(context.Background(), func(tx *Tx) error {
		var err error
		one, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "one"}})
		if err != nil {
			return err
		}
		two, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "two"}})
		if err != nil {
			return err
		}
		for _, item := range []model.InboxItem{
			{ID: "one-a", SessionID: one, Kind: "finished", TS: at},
			{ID: "one-b", SessionID: one, Kind: "finished", TS: at.Add(time.Second)},
			{ID: "acked", SessionID: two, Kind: "finished", TS: at, AckedAt: &at},
			{ID: "ended", SessionID: two, Kind: "finished", TS: at, EndedAt: &at},
			{ID: "wait", SessionID: two, Kind: "waiting", TS: at},
		} {
			if err := tx.UpsertInboxItem(item); err != nil {
				return err
			}
		}
		remote := tx.ForMachine("remote")
		id, err := remote.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "remote"}})
		if err != nil {
			return err
		}
		return remote.UpsertInboxItem(model.InboxItem{ID: "remote", SessionID: id, Kind: "finished", TS: at})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]int64{nil, {one}, {one, one}, {two}, {}} {
		got, err := st.UnseenFinishedSessions(ids)
		if err != nil {
			t.Fatal(err)
		}
		want := ids == nil || len(ids) > 0 && ids[0] == one
		if got[one] != want || len(got) != map[bool]int{true: 1, false: 0}[want] {
			t.Fatalf("ids=%v got=%v", ids, got)
		}
	}
}

func TestLabelProcsReadFreshIdentityWithoutArgv(t *testing.T) {
	st, err := Open("", Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Unix(1700000000, 0)
	err = st.Tx(context.Background(), func(tx *Tx) error {
		if err := tx.ReplaceProcs([]model.Proc{{PID: 42, StartTime: at, CPUPercent: 7}, {PID: 43, StartTime: at, CPUPercent: 99}}); err != nil {
			return err
		}
		return tx.exec("UPDATE procs SET argv='not JSON' WHERE machine_id=?", tx.machineID)
	})
	if err != nil {
		t.Fatal(err)
	}
	ps, err := st.LabelProcs([]int32{42, 42})
	if err != nil || len(ps) != 1 || ps[0].PID != 42 || !ps[0].StartTime.Equal(at) || ps[0].CPUPercent != 7 {
		t.Fatalf("procs=%+v err=%v", ps, err)
	}
	if empty, err := st.LabelProcs(nil); err != nil || len(empty) != 0 {
		t.Fatalf("empty=%v err=%v", empty, err)
	}
}

func TestLabelInputsBoundSQLParameters(t *testing.T) {
	st, err := Open("", Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Now()
	var id int64
	err = st.Tx(context.Background(), func(tx *Tx) error {
		var err error
		id, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "fixture"}})
		if err != nil {
			return err
		}
		if err := tx.UpsertInboxItem(model.InboxItem{ID: "finished", Kind: "finished", SessionID: id, TS: at}); err != nil {
			return err
		}
		return tx.ReplaceProcs([]model.Proc{{PID: 42, StartTime: at, CPUPercent: 3}})
	})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, 40001)
	pids := make([]int32, 40001)
	for i := 0; i < 40000; i++ {
		ids[i] = int64(i + 1)
		pids[i] = int32(i + 1)
	}
	ids[40000] = id
	pids[40000] = 42
	got, err := st.UnseenFinishedSessions(ids)
	if err != nil || len(got) != 1 || !got[id] {
		t.Fatalf("completion query exceeds parameter budget: %v %v", got, err)
	}
	ps, err := st.LabelProcs(pids)
	if err != nil || len(ps) != 1 || ps[0].PID != 42 {
		t.Fatalf("process query exceeds parameter budget or duplicates members: %v %v", ps, err)
	}
}
