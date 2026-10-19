package fleet

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestSummarySnapshotGateAndReplication(t *testing.T) {
	s, st, _ := serverFixture(t)
	s.cfg.CaptureText = "snippets"
	now := time.Now().UTC()
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		repo, err := tx.UpsertRepo(model.Repo{Name: "fixture", Path: "/fixture"})
		if err != nil {
			return err
		}
		for _, used := range []bool{false, true} {
			text := "titles-only summary"
			if used {
				text = "SNIPPET-DERIVED-SENTINEL"
			}
			if err := tx.UpsertSummary(model.Summary{ProjectID: repo, Text: text, UsedSnippets: used, CreatedAt: now, InputHash: "hash", StateHash: "state", Model: "haiku"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, share := range []string{"none", "titles", "snippets"} {
		s.cfg.Fleet.ShareText = share
		snap, err := s.snapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		b := string(raw(t, snap))
		if strings.Contains(b, "SNIPPET-DERIVED-SENTINEL") != (share == "snippets") {
			t.Fatal(share, b)
		}
		if strings.Contains(b, "titles-only summary") != (share != "none") {
			t.Fatal(share, b)
		}
		if strings.Contains(b, "project_id") {
			t.Fatal("integer ID leaked", b)
		}
		viewer := remoteStore(t)
		apply(t, viewer, Batch{MachineID: st.MachineID(), Snapshot: snap}, now)
		rows, err := viewer.ReadMachine(st.MachineID()).Summaries(0)
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if share == "titles" {
			want = 1
		}
		if share == "snippets" {
			want = 2
		}
		if len(rows) != want {
			t.Fatal(share, rows)
		}
		// A policy downgrade snapshot must remove previously received snippets.
		empty := *snap
		empty.Data.Summaries = nil
		apply(t, viewer, Batch{MachineID: st.MachineID(), Snapshot: &empty}, now)
		rows, err = viewer.ReadMachine(st.MachineID()).Summaries(0)
		if err != nil || len(rows) != 0 {
			t.Fatal(rows, err)
		}
	}
}

func TestSummaryIncrementalNaturalKeys(t *testing.T) {
	st := remoteStore(t)
	now := time.Now().UTC()
	apply(t, st, Batch{MachineID: "remote", Snapshot: remoteSnapshot(t, now, "epoch")}, now)
	key, p := feed.Encode(feed.Summary, feed.SummaryRow{Summary: model.Summary{Text: "sentence", CreatedAt: now}, RepoPath: "/fixture"}, feed.Titles)
	apply(t, st, Batch{MachineID: "remote", Cursor: Cursor{"epoch", 1}, Rows: []feed.FeedRow{{Seq: 1, Kind: string(feed.Summary), Key: key, Payload: p}}}, now)
	rows, err := st.ReadMachine("remote").Summaries(0)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	_, p = feed.Encode(feed.Delete, feed.DeleteRow{Kind: feed.Summary, Key: key}, feed.None)
	apply(t, st, Batch{MachineID: "remote", Cursor: Cursor{"epoch", 2}, Rows: []feed.FeedRow{{Seq: 2, Kind: string(feed.Delete), Payload: p}}}, now)
	rows, err = st.ReadMachine("remote").Summaries(0)
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
}
