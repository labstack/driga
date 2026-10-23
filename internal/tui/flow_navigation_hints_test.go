package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/flowtest"
)

func TestFlowFooterAndHelpDescribeMapKeys(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	m.tab, m.width, m.height = flowTab, 140, 60
	m.flow = flowFixture()
	for _, text := range []string{m.footer()} {
		for _, want := range []string{"j/k session", "enter jump", "[/] family", "-/+ zoom"} {
			if !strings.Contains(text, want) {
				t.Errorf("missing %q: %s", want, text)
			}
		}
	}
	for _, want := range []string{"esc back", "? help", "q quit"} {
		if !strings.Contains(m.footer(), want) {
			t.Error("footer missing", want)
		}
	}
	m.help = true
	for _, stale := range []string{"j k worker", "j/k worker", "enter open", "[ / ] session"} {
		if strings.Contains(m.footer(), stale) || strings.Contains(m.View().Content, "7 Flow · [ / ] session") {
			t.Error("stale Flow help:", stale)
		}
	}
}

func TestFlowHintsAppearOnceInFooter(t *testing.T) {
	m := setup(t, fixture(), Options{NoColor: true, Now: func() time.Time { return flowtest.Now }}).(*app)
	m.flow = flowFixture()
	m.tab = flowTab
	m.loaded = true
	m.width = 140
	m.height = 80
	text := m.View().Content
	if strings.Count(text, "enter jump") != 1 {
		t.Fatal("flow repeats its keyboard hints", text)
	}
}
