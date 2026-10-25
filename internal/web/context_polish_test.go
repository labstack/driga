package web

import (
	"context"
	"html"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

func TestTailIncludesSessionContextWithoutHookText(t *testing.T) {
	s, st, agent, _ := fixture(t)
	session, err := st.Session(agent)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.Tx(context.Background(), func(tx *store.Tx) error {
		if err := tx.SetSourceOK("sockets", time.Now()); err != nil {
			return err
		}
		return tx.InsertEvent(model.Event{Adapter: session.Adapter, ExternalID: session.ExternalID, TS: time.Now(), Kind: "Stop"})
	}); err != nil {
		t.Fatal(err)
	}
	body := request(s, "GET", "/").Body.String()
	start := strings.Index(body, `data-event-key=`)
	if start < 0 {
		t.Fatal("missing event")
	}
	line := strings.SplitN(body[start:], "</div>", 2)[0]
	if !strings.Contains(line, "Fixture project") || !strings.Contains(line, html.EscapeString(session.Title)) || !strings.Contains(line, "Stop") {
		t.Fatal(line)
	}
	if !strings.Contains(body, `class="seg intake-status"`) {
		t.Fatal("missing compact intake segment")
	}
}

func TestProjectRiskIsInlineRowAnnotation(t *testing.T) {
	s, _, _, _ := fixture(t)
	p := pageData{}
	p.Projects.Rows = []view.ProjectRow{{Status: view.ProjectStatus{Repo: model.Repo{Name: "project"}, AtRisk: []string{">10 worktrees"}}, LatestHandoff: "Review contracts"}}
	p.prepare()
	var html strings.Builder
	if err := s.templates.ExecuteTemplate(&html, "projectCards", p); err != nil {
		t.Fatal(err)
	}
	body := html.String()
	at := strings.Index(body, `class="row-annotation d"`)
	if at < 0 || !strings.Contains(body, `&gt;10 worktrees`) || strings.Contains(body, `<p class="notice"`) || strings.Contains(body[:at], "</a>") {
		t.Fatal(body)
	}
}
