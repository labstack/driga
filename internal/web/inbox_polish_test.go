package web

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/viewer"
)

func TestInboxDenseSectionsAndCap(t *testing.T) {
	s, _, _, _ := fixture(t)
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	rows := []view.InboxRow{}
	agents := []view.AgentRow{}
	// Deliberately interleave unsorted completions and more than 50 waits.
	for i := 0; i < 494; i++ {
		kind := "finished"
		if i%7 == 0 {
			kind = "waiting"
			agents = append(agents, view.AgentRow{MachineID: "local", ID: int64(i + 1), Label: "waiting", LastActivityAt: now.Add(-time.Duration(i) * time.Minute)})
		}
		rows = append(rows, view.InboxRow{MachineID: "local", MachineFresh: view.MachineFresh{MachineName: "workstation"}, InboxItem: model.InboxItem{
			ID: fmt.Sprintf("item-%03d", i), SessionID: int64(i + 1), Kind: kind, TS: now.Add(-time.Duration(i) * time.Minute), Summary: "turn finished", Reason: "permission", Tool: "Bash",
			Jump: &model.InboxJump{Cwd: "/very/long/private/project/directory", Command: "agent --resume 'long command <safe>'"},
		}, Project: "project", Branch: "main", Title: "Review changes", Age: time.Duration(i) * time.Minute})
	}
	for _, scope := range []string{"all", "local"} {
		p := pageData{Page: "/inbox", Scope: scope, Snapshot: viewer.Snapshot{ObservedAt: now, Inbox: view.InboxView{Rows: rows}, Agents: view.AgentsView{Rows: agents}}}
		p.prepare()
		var out strings.Builder
		if err := s.templates.ExecuteTemplate(&out, "inbox", p); err != nil {
			t.Fatal(err)
		}
		body := out.String()
		for _, want := range []string{`data-inbox-section="needs"`, `data-inbox-section="recent"`, "needs you", "71 waiting · oldest 8h10m0s", "recent", "reason", "tool", "wait / age", "project · branch", "title / detail", "time", `data-inbox-older`, `data-inbox-copy`, `data-inbox-command`, `aria-readonly="true"`, "&lt;safe&gt;"} {
			if !strings.Contains(body, want) {
				t.Errorf("missing %q", want)
			}
		}
		if strings.Contains(body, `data-inbox-machine`) != (scope == "all") {
			t.Error("machine column must be fleet-only")
		}
		matches := regexp.MustCompile(`<details\b[^>]*data-inbox-key="[^"]*"[^>]*>`).FindAllString(body, -1)
		if len(matches) != 494 {
			t.Fatalf("got %d rows", len(matches))
		}
		visible, needs := 0, 0
		for _, tag := range matches {
			if !strings.Contains(tag, " hidden") {
				visible++
			}
			if !strings.Contains(tag, "data-inbox-recent") {
				needs++
				if strings.Contains(tag, " hidden") {
					t.Error("wait was capped")
				}
			}
		}
		if needs != 71 || visible != 121 {
			t.Errorf("needs=%d visible=%d; want 71 + 50", needs, visible)
		}
		if !strings.Contains(matches[0], "item-490") || !strings.Contains(matches[70], "item-000") || !strings.Contains(matches[71], "item-001") {
			t.Error("waits must precede newest-first recent rows, oldest wait first")
		}
		for _, summary := range regexp.MustCompile(`(?s)<summary\b.*?</summary>`).FindAllString(body, -1) {
			if strings.Contains(summary, "<a ") || strings.Contains(summary, "/very/long") {
				t.Error("summary contains a link or full cwd")
			}
		}
		if regexp.MustCompile(`(?i)<style\b|<script\b|\sstyle=|\son[a-z]+=`).MatchString(body) {
			t.Error("inline script/style/handler")
		}
	}
}

func TestInboxReadOnlyCSPAndRefresh(t *testing.T) {
	s, _, _, _ := fixture(t)
	w := request(s, "GET", "/inbox")
	if w.Header().Get("Content-Security-Policy") != request(s, "GET", "/").Header().Get("Content-Security-Policy") {
		t.Error("inbox changed CSP")
	}
	if !strings.Contains(w.Body.String(), `hx-trigger="console:refresh"`) {
		t.Error("lost live refresh")
	}
	if request(s, "POST", "/inbox").Code != 405 {
		t.Error("inbox accepts POST")
	}
}

func TestInboxWaitGenerationAndVisibility(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	closed := now.Add(-time.Minute)
	p := pageData{Snapshot: viewer.Snapshot{ObservedAt: now, Inbox: view.InboxView{Rows: []view.InboxRow{
		{InboxItem: model.InboxItem{ID: "new-event-old-wait", Kind: "waiting", TS: now, WaitingSinceAt: now.Add(-time.Hour)}},
		{InboxItem: model.InboxItem{ID: "old-event-new-wait", Kind: "waiting", TS: now.Add(-2 * time.Hour), WaitingSinceAt: now.Add(-time.Minute)}},
		{InboxItem: model.InboxItem{ID: "ended", Kind: "waiting", EndedAt: &closed}},
		{InboxItem: model.InboxItem{ID: "acked", Kind: "waiting", AckedAt: &closed}},
	}}}}
	p.prepare()
	if len(p.InboxWaiting) != 2 || len(p.InboxRecent) != 0 || p.InboxWaiting[0].ID != "new-event-old-wait" || p.InboxWaiting[0].Waited != time.Hour {
		t.Fatalf("wrong active wait order/age: %+v", p.InboxWaiting)
	}
}

func TestInboxRecentCapBoundaries(t *testing.T) {
	s, _, _, _ := fixture(t)
	for _, count := range []int{0, 50, 51} {
		p := pageData{Scope: "local"}
		for i := 0; i < count; i++ {
			p.InboxRecent = append(p.InboxRecent, view.InboxRow{InboxItem: model.InboxItem{ID: fmt.Sprint(i), Kind: "finished"}})
		}
		var out strings.Builder
		if err := s.templates.ExecuteTemplate(&out, "inbox", p); err != nil {
			t.Fatal(err)
		}
		body := out.String()
		if strings.Count(body, " hidden") != max(0, count-50) || strings.Contains(body, "data-inbox-older") != (count > 50) {
			t.Errorf("incorrect cap/control at %d rows", count)
		}
		if count == 0 && (!strings.Contains(body, "Nothing needs you.") || !strings.Contains(body, "No recent completions.")) {
			t.Error("missing empty states")
		}
	}
}

func TestInboxJavaScript(t *testing.T) {
	runtime, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node JavaScript runner unavailable")
	}
	js, err := assets.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	harness, err := os.ReadFile("testdata/inbox.js")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(runtime, "-")
	cmd.Stdin = strings.NewReader(string(js) + "\n" + string(harness))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("inbox behavior: %v\n%s", err, out)
	}
}
