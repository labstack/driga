package view

import (
	"bytes"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestEndedPresentationAndFilters(t *testing.T) {
	if e := tailEvent(model.Event{Kind: "SessionEnd"}, "local", "local", ""); e.State != "ended" {
		t.Errorf("SessionEnd tail state = %s", e.State)
	}
	counts := StateCounts([]AgentRow{{Label: "ended"}, {Label: "dead"}})
	if len(counts) != 2 || counts[0].Label != "ended" || counts[0].Count != 1 {
		t.Errorf("ended missing from totals: %+v", counts)
	}
	if FlowGlyph("ended") != "●" || FlowHeadPattern("ended") != '━' {
		t.Error("ended missing from flow glyphs")
	}
	color := StateStyle("ended", false).GetForeground()
	wantColor := ThemeColor("finished")
	r, g, b, a := color.RGBA()
	wr, wg, wb, wa := wantColor.RGBA()
	if r != wr || g != wg || b != wb || a != wa {
		t.Error("ended must use the neutral completion color")
	}
	s := fixture(t, func(tx *store.Tx) error {
		id, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "ended", LastActivityAt: testNow}, Label: "ended", EndedAt: &testNow})
		if err != nil {
			return err
		}
		return tx.UpsertInboxItem(model.InboxItem{ID: "completion", SessionID: id, Kind: "finished", TS: testNow})
	})
	for _, all := range []bool{false, true} {
		v, err := Agents(s, AgentFilter{All: all}, testNow)
		if err != nil || len(v.Rows) != map[bool]int{false: 0, true: 1}[all] {
			t.Fatalf("all=%v: %+v %v", all, v, err)
		}
		if all {
			var out bytes.Buffer
			if err := Render(&out, v, RenderOpts{NoColor: true}); err != nil || !strings.Contains(out.String(), "ended") {
				t.Fatalf("table: %s %v", out.String(), err)
			}
		}
	}
	v, err := Inbox(s, Filter{}, testNow)
	if err != nil || len(v.Rows) != 1 || v.Rows[0].Kind != "finished" {
		t.Fatalf("ended lost unread completion: %+v %v", v, err)
	}
}
