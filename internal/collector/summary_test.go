package collector

import (
	"context"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
)

func TestSummarySpoolFeedGate(t *testing.T) {
	for _, share := range []string{"titles", "snippets", "none"} {
		for _, onlySpool := range []bool{false, true} {
			t.Run(share+map[bool]string{true: "-spool", false: "-pass"}[onlySpool], func(t *testing.T) {
				d, st, _, a := setup(t)
				a.obs = nil
				d.Cfg.CaptureText = "snippets"
				d.Cfg.Fleet.ShareText = share
				d.Cfg.Fleet.Serve = true
				var repo int64
				if err := st.Tx(context.Background(), func(tx *store.Tx) error {
					var err error
					repo, err = tx.UpsertRepo(model.Repo{Path: "/fixture", Name: "fixture"})
					return err
				}); err != nil {
					t.Fatal(err)
				}
				for _, used := range []bool{false, true} {
					text := "titles summary"
					if used {
						text = "SNIPPET-SENTINEL"
					}
					row := model.Summary{ProjectID: repo, Text: text, UsedSnippets: used, CreatedAt: d.now(), InputHash: "hash", StateHash: "state", Model: "haiku"}
					if err := spool.Append(d.SpoolDir, model.HookEvent{MachineID: st.MachineID(), TS: d.now(), Event: "summary", Summary: &row}); err != nil {
						t.Fatal(err)
					}
				}
				if onlySpool {
					if err := drainOnly(context.Background(), d, st); err != nil {
						t.Fatal(err)
					}
				} else {
					pass(t, d, st, Plan{})
				}
				rows, err := st.Summaries(repo)
				if err != nil || len(rows) != 2 {
					t.Fatal(rows, err)
				}
				foundTitles, foundSnippets := false, false
				for _, r := range feedRows(t, st, 0) {
					if r.Kind == string(feed.Summary) {
						foundTitles = foundTitles || strings.Contains(string(r.Payload), "titles summary")
						foundSnippets = foundSnippets || strings.Contains(string(r.Payload), "SNIPPET-SENTINEL")
					}
				}
				if foundTitles != (share != "none") || foundSnippets != (share == "snippets") {
					t.Fatal(share, foundTitles, foundSnippets)
				}
			})
		}
	}
}
