package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
)

func TestHomeInitialAndKeyZero(t *testing.T) {
	m := setup(t, fixture(), Options{})
	if m.(*app).tab != homeTab || !strings.Contains(m.View().Content, "0 Home") {
		t.Fatal(m.View().Content)
	}
	m = key(m, '1')
	m = key(m, '0')
	if m.(*app).tab != homeTab || !strings.Contains(m.View().Content, "0 Home") {
		t.Fatal(m.View().Content)
	}
	m = key(m, '?')
	if !strings.Contains(m.View().Content, "0 Home") {
		t.Fatal(m.View().Content)
	}
}

func TestHomeWidths(t *testing.T) {
	for _, width := range []int{80, 120, 160} {
		s := fixture()
		s.snap.Machine.Sample = model.MachineSample{TS: s.snap.ObservedAt, MemUsed: 40 << 20, MemTotal: 80 << 20, SwapUsed: 1 << 20, SwapTotal: 4 << 20, Load1: 2, Cores: 4, DiskFree: 80 << 30}
		s.snap.Activity.Machines = []view.ActivityMachine{{Buckets: make([]view.ActivityBucket, 60)}}
		s.snap.Activity.Machines[0].Buckets[59] = view.ActivityBucket{Sample: &s.snap.Machine.Sample, Events: 3}
		m := setup(t, s, Options{})
		m, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: 100})
		text := m.View().Content
		for _, title := range []string{"Needs you", "Agents", "Machine", "Activity", "Projects", "Leftovers"} {
			if !strings.Contains(text, title) {
				t.Errorf("width %d missing %s", width, title)
			}
		}
		for _, line := range strings.Split(text, "\n") {
			if lipgloss.Width(line) > width {
				t.Fatalf("width %d: %q", width, line)
			}
		}
		if strings.Contains(text, "\x1b") {
			t.Fatal("ANSI with NoColor")
		}
		if !strings.Contains(text, "····") {
			t.Fatal("sample gaps lost", text)
		}
	}
}

func TestHomeWaitingAndStalledWithoutInbox(t *testing.T) {
	s := fixture()
	s.snap.Inbox.Rows = nil
	s.snap.Agents.Rows[0].Label = "waiting"
	s.snap.Agents.Rows = append(s.snap.Agents.Rows, view.AgentRow{ID: 2, Label: "~stalled", Title: "stalled agent"})
	s.snap.AgentDetails[2] = view.AgentView{Session: model.Session{ID: 2, SessionObs: model.SessionObs{LastActivityAt: s.snap.ObservedAt}}}
	m := setup(t, s, Options{Spool: func(model.HookEvent) error { t.Fatal("invented ack event"); return nil }}).(*app)
	if len(m.rows) != 2 || m.rows[0].agent != 2 {
		t.Fatalf("rows: %+v", m.rows)
	}
	_, cmd := press(m, 'x')
	if cmd != nil || !strings.Contains(m.status, "no inbox event") {
		t.Fatal(m.status)
	}
}

func TestHomeNeedsYouNewestFirstAndActions(t *testing.T) {
	s := fixture()
	now := s.snap.ObservedAt
	s.snap.Inbox.Rows = []view.InboxRow{
		{InboxItem: model.InboxItem{ID: "old", SessionID: 1, Kind: "finished", TS: now.Add(-time.Hour), Summary: "old completion"}},
		{InboxItem: model.InboxItem{ID: "new", SessionID: 1, Kind: "waiting", TS: now, Summary: "new attention"}},
		{InboxItem: model.InboxItem{ID: "acked", Kind: "finished", TS: now, AckedAt: &now}},
	}
	var ack string
	m := setup(t, s, Options{Spool: func(e model.HookEvent) error { ack = e.Message; return nil }})
	a := m.(*app)
	if len(a.rows) != 1 || a.rows[0].inbox.ID != "new" {
		t.Fatalf("rows: %+v", a.rows)
	}
	m = key(m, tea.KeyEnter)
	if ack != "" || !strings.Contains(m.View().Content, "hook notification") {
		t.Fatal(m.View().Content, ack)
	}
	m, cmd := press(m, 'x')
	execute(t, m, cmd)
	if ack != "new" {
		t.Fatal(ack)
	}
}

func TestAgentsSplitThresholdAndCursor(t *testing.T) {
	s := fixture()
	s.snap.Agents.Rows = append(s.snap.Agents.Rows, view.AgentRow{ID: 2, Label: "working", Title: "second"})
	s.snap.AgentDetails[2] = view.AgentView{Session: model.Session{ID: 2, Evidence: "second evidence"}}
	for _, width := range []int{139, 140, 160} {
		m := setup(t, s, Options{})
		m = key(m, '1')
		m, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: 60})
		if strings.Contains(m.View().Content, "Evidence:") != (width >= 140) {
			t.Fatalf("width %d: %s", width, m.View().Content)
		}
		if width >= 140 {
			m = key(m, 'j')
			if !strings.Contains(m.View().Content, "second evidence") {
				t.Fatal(m.View().Content)
			}
		} else {
			m = key(m, tea.KeyEnter)
			if !strings.Contains(m.View().Content, "hook notification") {
				t.Fatal(m.View().Content)
			}
			m = key(m, tea.KeyEscape)
			if strings.Contains(m.View().Content, "Evidence:") {
				t.Fatal(m.View().Content)
			}
		}
	}
}

func TestHomePanelsFitWithoutClipping(t *testing.T) {
	s := fixture()
	s.snap.Agents.Rows[0].Title = strings.Repeat("界é", 100)
	s.snap.Inbox.Rows[0].Summary = strings.Repeat("界é", 100)
	for _, width := range []int{80, 109, 110, 120, 139, 140, 149, 150, 160} {
		m := setup(t, s, Options{}).(*app)
		m.Update(tea.WindowSizeMsg{Width: width, Height: 100})
		for _, line := range strings.Split(m.homeView(), "\n") {
			if lipgloss.Width(line) > width {
				t.Fatalf("width %d: %q", width, line)
			}
		}
		if strings.Count(strings.Split(m.homeView(), "\n")[0], "╭") != homeColumns(width) {
			t.Fatal("wrong column count", width)
		}
		if strings.Contains(bar(1, 1, 20, lipgloss.NewStyle()), "\n") {
			t.Fatal("bar wraps onto another line")
		}
	}
}

func TestAgentDetailUsesStateColors(t *testing.T) {
	s := fixture()
	d := s.snap.AgentDetails[1]
	d.Label = "stalled"
	s.snap.AgentDetails[1] = d
	m := setup(t, s, Options{}).(*app)
	m.opts.NoColor = false
	key(m, tea.KeyEnter)
	if !strings.Contains(m.View().Content, view.StateStyle("stalled", false).Render("stalled")) {
		t.Fatalf("detail state lacks red: %q", m.View().Content)
	}
}

func TestHomeHonorsNO_COLOR(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := New(fixture(), Options{DataDir: t.TempDir()}).(*app)
	m.Update(refreshMsg{snap: fixture().snap, changed: true})
	if !m.opts.NoColor || strings.Contains(m.View().Content, "\x1b") {
		t.Fatal("NO_COLOR ignored")
	}
}

func TestNOColorEmptyMeansUnset(t *testing.T) {
	for _, value := range []string{"", "0", "1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("NO_COLOR", value)
			m := New(&fakeSource{}, Options{}).(*app)
			if m.opts.NoColor != (value != "") {
				t.Fatalf("NO_COLOR=%q disables color: %v", value, m.opts.NoColor)
			}
		})
	}
}

func TestHomeFleetAttentionKeepsFields(t *testing.T) {
	s := fleetSource()
	s.snap.Inbox.Rows[0].MachineID = "local"
	s.snap.Inbox.Rows = append(s.snap.Inbox.Rows, view.InboxRow{MachineID: "remote", Project: "peer-project", InboxItem: model.InboxItem{ID: "peer", SessionID: 2, Kind: "waiting", Summary: "peer needs input", TS: s.snap.ObservedAt}})
	m := setup(t, s, Options{}).(*app)
	if len(m.rows) != 2 || m.rows[0].cells[1] != "waiting" || m.rows[0].cells[2] != "peer-project" {
		t.Fatalf("attention fields shifted: %+v", m.rows)
	}
	if !strings.Contains(m.View().Content, "remote title") || !strings.Contains(m.View().Content, "peer-project") {
		t.Fatal(m.View().Content)
	}
}

func TestHomeCurrentAttentionNotHiddenByCompletion(t *testing.T) {
	s := fixture()
	s.snap.Inbox.Rows[0].Kind = "finished"
	s.snap.Agents.Rows[0].Label = "~stalled"
	m := setup(t, s, Options{}).(*app)
	if len(m.rows) != 1 || m.rows[0].cells[1] != "~stalled" {
		t.Fatalf("session must appear once with its current stalled state: %+v", m.rows)
	}
}

func TestHomeLoadsLeftoversAndScrolls(t *testing.T) {
	s := fixture()
	m := New(s, Options{DataDir: t.TempDir(), NoColor: true, CleanupList: func(context.Context) ([]cleanup.Candidate, error) {
		return []cleanup.Candidate{{Kind: cleanup.Worktree, Status: "ready"}, {Kind: cleanup.Worktree, Status: "refused"}}, nil
	}})
	m, cmd := m.Update(m.(*app).poll()())
	if cmd != nil {
		m = execute(t, m, cmd)
	}
	if m.(*app).snap.Cleanup == nil {
		t.Fatal("Home did not load leftovers")
	}
	m = key(m, tea.KeyPgDown)
	m = key(m, tea.KeyPgDown)
	if !strings.Contains(m.View().Content, "1 ready · 1 kept") {
		t.Fatal(m.View().Content)
	}
}

func TestHomeGaugeTrackAndPercent(t *testing.T) {
	s := fixture()
	s.snap.Machine.Sample = model.MachineSample{TS: s.snap.ObservedAt, MemUsed: 90, MemTotal: 100, SwapUsed: 3, SwapTotal: 4}
	m := setup(t, s, Options{}).(*app)
	text := m.homeView()
	for _, part := range []string{"90%", "75%", "░"} {
		if !strings.Contains(text, part) {
			t.Errorf("missing gauge %q", part)
		}
	}
}

func TestActivityFullWidthAndEmptyNotes(t *testing.T) {
	buckets := make([]view.ActivityBucket, 60)
	buckets[59] = view.ActivityBucket{Events: 3, Sample: &model.MachineSample{Load1: 2}}
	if got := lipgloss.Width(activitySpark(buckets, 90, true)); got != 90 {
		t.Errorf("spark width %d, want full available width 90", got)
	}
	for _, withBuckets := range []bool{false, true} {
		s := fixture()
		if withBuckets {
			s.snap.Activity.Machines = []view.ActivityMachine{{Buckets: make([]view.ActivityBucket, 60)}}
		}
		m := setup(t, s, Options{}).(*app)
		for _, note := range []string{"History builds while the collector runs", "Events need agent hooks: driga hooks install"} {
			if !strings.Contains(m.homeView(), note) {
				t.Errorf("missing note %q", note)
			}
		}
	}
}

func TestFooterCwdsAndPendingOTLP(t *testing.T) {
	s := fixture()
	s.snap.Freshness = map[string]view.Fresh{"cwds": {LastOKAt: s.snap.ObservedAt.Add(-20 * time.Second)}}
	m := setup(t, s, Options{}).(*app)
	if strings.Contains(m.footer(), "stale cwds") {
		t.Fatal("cwd source uses procs cadence")
	}
}

func TestHomeProjectsOrderAndAggregation(t *testing.T) {
	s := fixture()
	s.snap.Projects.Rows = []view.ProjectRow{
		{Status: view.ProjectStatus{Repo: model.Repo{ID: 1, Name: "Quiet project"}, Activity: "idle"}},
		{Status: view.ProjectStatus{Repo: model.Repo{ID: 2, Name: "Recent project"}, Where: []view.WherePart{{At: s.snap.ObservedAt}}}},
		{Status: view.ProjectStatus{Repo: model.Repo{ID: 3, Name: "Live project"}, LiveAgents: 6}},
	}
	s.snap.Agents.Rows = nil
	for i := 0; i < 6; i++ {
		state := "idle"
		if i < 2 {
			state = "working"
		}
		s.snap.Agents.Rows = append(s.snap.Agents.Rows, view.AgentRow{ID: int64(i + 1), Label: state, Project: "Live project", RepoID: 3})
	}
	m := setup(t, s, Options{}).(*app)
	m.width, m.height = 160, 44
	m.rebuild()
	text := m.homeView()
	if strings.Index(text, "Live project") > strings.Index(text, "Recent project") || strings.Index(text, "Recent project") > strings.Index(text, "Quiet project") {
		t.Error("wrong project order")
	}
	if !strings.Contains(text, "2 working · 4 idle") {
		t.Error("missing aggregated states")
	}
	m.opts.NoColor = false
	if !strings.Contains(m.homeView(), "\x1b[2mQuiet project") {
		t.Error("quiet project is not dimmed")
	}
}

func TestHomeProjectsFitAvailableHeight(t *testing.T) {
	for _, size := range [][2]int{{160, 44}, {120, 40}} {
		src := fixture()
		for i := 0; i < 50; i++ {
			src.snap.Projects.Rows = append(src.snap.Projects.Rows, view.ProjectRow{Status: view.ProjectStatus{Repo: model.Repo{ID: int64(i + 1), Name: fmt.Sprint("project", i)}}})
		}
		m := setup(t, src, Options{}).(*app)
		m.width, m.height = size[0], size[1]
		m.rebuild()
		body := m.homeView()
		if lipgloss.Height(body) > m.bodyHeight() {
			t.Fatalf("%dx%d: body height %d > %d", m.width, m.height, lipgloss.Height(body), m.bodyHeight())
		}
		out := m.View().Content
		if !strings.Contains(out, "╰") {
			t.Fatalf("bottom border missing: %s", out)
		}
	}
}

func TestHomeSameNamedReposStaySeparate(t *testing.T) {
	src := fixture()
	src.snap.Inbox.Rows = nil
	src.snap.Projects.Rows = []view.ProjectRow{{Status: view.ProjectStatus{Repo: model.Repo{ID: 1, Name: "app", Path: "/a/app"}}}, {Status: view.ProjectStatus{Repo: model.Repo{ID: 2, Name: "app", Path: "/b/app"}}}}
	src.snap.Agents.Rows = []view.AgentRow{{ID: 1, RepoID: 1, Project: "app", Branch: "first-branch", Label: "working"}, {ID: 2, RepoID: 2, Project: "app", Branch: "second-branch", Label: "waiting"}}
	m := setup(t, src, Options{}).(*app)
	m.width = 160
	m.height = 44
	m.rebuild()
	body := m.homeView()
	if !strings.Contains(body, "second-branch") {
		t.Fatalf("mixed repos: %s", body)
	}
}

func TestEventDrivenHelpReportsLastEvent(t *testing.T) {
	s := fixture()
	at := s.snap.ObservedAt.Add(-24 * time.Hour)
	s.snap.Freshness = map[string]view.Fresh{"otlp": {LastOKAt: at, Status: "last_event"}, "sockets": {LastOKAt: at, Status: "last_event"}}
	m := setup(t, s, Options{}).(*app)
	key(m, '?')
	if strings.Contains(m.footer(), "last event") || !strings.Contains(m.header(), "intake · receiving") || !strings.Contains(m.View().Content, "last event") || strings.Contains(m.View().Content, "stale") {
		t.Fatal(m.View().Content)
	}
}

func TestHomeTinyWidths(t *testing.T) {
	for _, width := range []int{-1, 0, 1, 2, 3, 4, 8, 16, 30} {
		for _, empty := range []bool{false, true} {
			s := fixture()
			if empty {
				s.snap.Agents.Rows = nil
			}
			m := setup(t, s, Options{})
			m, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: 10})
			_ = m.View()
		}
	}
	m := setup(t, fixture(), Options{})
	m = key(m, '?')
	if !strings.Contains(m.View().Content, "1–8 tabs") {
		t.Fatal(m.View().Content)
	}
}
