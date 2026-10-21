package collector

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestCollectorStartedFromStoredOnce(t *testing.T) {
	d, st, p, _ := setup(t)
	p.procs = append(p.procs, model.Proc{PID: 2, Exe: "/Applications/Fixture Terminal.app/Contents/MacOS/terminal", StartTime: p.procs[0].StartTime.Add(-time.Hour)})
	pass(t, d, st, Plan{Procs: true})
	if got := sessions(t, st)[0].StartedFrom; got != "Fixture Terminal" {
		t.Fatalf("first match: %q", got)
	}
	p.procs[1].Exe = "/Applications/Other Editor.app/Contents/MacOS/editor"
	pass(t, d, st, Plan{Procs: true})
	pass(t, d, st, Plan{})
	got := sessions(t, st)[0]
	if got.StartedFrom != "Fixture Terminal" {
		t.Fatalf("overwritten: %+v", got)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["started_from"] != "Fixture Terminal" {
		t.Fatalf("JSON: %s", b)
	}
	d.Notifier = &fakeNotifier{}
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		return tx.UpsertInboxItem(model.InboxItem{ID: "wait", SessionID: got.ID, Kind: "waiting", TS: d.now().Add(-time.Hour)})
	}); err != nil {
		t.Fatal(err)
	}
	if err := alertInbox(context.Background(), d, st); err != nil {
		t.Fatal(err)
	}
	n := d.Notifier.(*fakeNotifier)
	if len(n.notes) != 1 || !strings.HasSuffix(n.notes[0].Body, " · from Fixture Terminal") {
		t.Fatal(n.notes)
	}
	if p.procCalls != 2 {
		t.Fatalf("extra process scans: %d", p.procCalls)
	}
}

func TestCollectorStartedFromRequiresMatchingLiveProcess(t *testing.T) {
	for _, tc := range []string{"reused session pid", "hosted", "missing bundle then matched"} {
		t.Run(tc, func(t *testing.T) {
			d, st, p, a := setup(t)
			p.procs = append(p.procs, model.Proc{PID: 2, Exe: "/Applications/Fixture Editor.app/Contents/MacOS/editor", StartTime: p.procs[0].StartTime.Add(-time.Hour)})
			switch tc {
			case "reused session pid":
				a.obs[0].ProcStart = p.procs[0].StartTime.Add(-time.Hour)
			case "hosted":
				a.obs[0].Hosted = true
			default:
				p.procs[1].Exe = "/usr/bin/sh"
			}
			pass(t, d, st, Plan{Procs: true})
			if got := sessions(t, st)[0].StartedFrom; got != "" {
				t.Fatalf("unproved surface: %q", got)
			}
			if tc == "missing bundle then matched" {
				p.procs[1].Exe = "/Applications/Fixture Editor.app/Contents/MacOS/editor"
				pass(t, d, st, Plan{Procs: true})
				if got := sessions(t, st)[0].StartedFrom; got != "Fixture Editor" {
					t.Fatalf("later match: %q", got)
				}
			}
		})
	}
}
