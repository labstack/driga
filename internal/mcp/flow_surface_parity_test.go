package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/labstack/driga/internal/flowtest"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/tui"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/viewer"
)

type selectedFlowSource struct {
	*viewer.StoreSource
	id int64
}

func (s selectedFlowSource) LoadFlow(ctx context.Context, scope store.Scope, _ int64) (view.FlowView, error) {
	return s.StoreSource.LoadFlow(ctx, scope, s.id)
}

func TestFlowSnapshotMatchesAllSurfaces(t *testing.T) {
	st, _ := fixture(t)
	var id int64
	if err := st.Tx(context.Background(), func(tx *store.Tx) (err error) { id, err = flowtest.Seed(tx); return err }); err != nil {
		t.Fatal(err)
	}
	expected := flowtest.Expected()
	want, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	checkJSON := func(surface string, raw []byte) {
		t.Helper()
		var envelope struct {
			Data struct{ Mixer json.RawMessage }
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Fatal(err)
		}
		if string(envelope.Data.Mixer) != string(want) {
			t.Errorf("%s mixer: got %s, want %s", surface, envelope.Data.Mixer, want)
		}
	}
	checkJSON("CLI", cliJSON(t, []string{"flow", fmt.Sprint(id)}))
	result := call(t, connect(t, globals()), toolCase{name: "get_flow", args: map[string]any{"id": id, "limit": 500}})
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	checkJSON("MCP", raw)

	source := viewer.NewStoreSource(st, t.TempDir(), view.Filter{All: true})
	source.Now = func() time.Time { return flowtest.Now.Add(5 * time.Second) }
	m := tui.New(selectedFlowSource{source, id}, tui.Options{NoColor: true, DataDir: t.TempDir(), Now: source.Now})
	initMsg := m.Init()()
	if batch, ok := initMsg.(tea.BatchMsg); ok {
		initMsg = batch[0]()
	}
	m, _ = m.Update(initMsg)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 100})
	m, cmd := m.Update(tea.KeyPressMsg{Code: '0', Text: "0"})
	if cmd != nil {
		m, _ = m.Update(cmd())
	}
	if !strings.Contains(m.View().Content, "NEEDS YOU") {
		t.Fatal("TUI did not open board")
	}
	m, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("TUI did not open family from board")
	}
	m, _ = m.Update(cmd())
	frame := m.View().Content
	if !strings.Contains(frame, fmt.Sprintf("verdict finished · %d of 4 agree", expected.Agreement)) {
		t.Errorf("TUI agreement/hidden count differs from fixture: %s", frame)
	}
	for _, row := range expected.Rows {
		if !strings.Contains(frame, row.Reading) {
			t.Errorf("TUI missing signal %s = %q", row.Name, row.Reading)
		}
	}
}
