package collector

import (
	"context"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
)

func TestSpoolOnlyPassPreservesHookInboxAndHandoff(t *testing.T) {
	d, st, _, _ := setup(t)
	d.Cfg.CaptureText = "snippets"
	pass(t, d, st, Plan{Procs: true, Sockets: true})
	var repo int64
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		var err error
		repo, err = tx.UpsertRepo(model.Repo{Path: t.TempDir()})
		if err != nil {
			return err
		}
		ss, err := tx.Sessions(store.SessionFilter{})
		if err != nil {
			return err
		}
		ss[0].RepoID = &repo
		_, err = tx.UpsertSession(ss[0])
		return err
	}); err != nil {
		t.Fatal(err)
	}
	now := d.Now().Add(time.Second)
	d.Now = func() time.Time { return now }
	if err := spool.Append(d.SpoolDir, model.HookEvent{TS: now, Adapter: "claude", SessionID: "one", Event: "Stop", Message: "completed"}); err != nil {
		t.Fatal(err)
	}
	if err := drainOnly(context.Background(), d, st); err != nil {
		t.Fatal(err)
	}
	items := inbox(t, st)
	if len(items) != 1 || items[0].Summary != "completed" {
		t.Fatalf("spool-only completion lost: %+v", items)
	}
	handoffs, err := st.Handoffs(repo, 0)
	if err != nil || len(handoffs) != 1 || handoffs[0].Text != "completed" {
		t.Fatalf("spool-only handoff lost: %+v %v", handoffs, err)
	}
}

// A single snapshot pass can never verify an orphan, so it must not pay for
// the socket probe (it dominated snapshot latency on a loaded machine).
