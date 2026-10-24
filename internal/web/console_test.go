package web

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/viewer"
)

func TestConsoleStructureAndCSP(t *testing.T) {
	s, _, agent, project := fixture(t)
	inline := regexp.MustCompile(`(?i)<script[^>]*>\s*[^<\s]|\sstyle=|\son[a-z]+=`)
	for _, path := range []string{"/", "/agents", fmt.Sprint("/agents/", agent), "/projects", fmt.Sprint("/projects/", project), "/inbox", "/orphans", "/machines", "/cleanup", "/activity"} {
		w := request(s, "GET", path, s.token)
		if w.Code != 200 {
			t.Errorf("%s: %d", path, w.Code)
			continue
		}
		body := w.Body.String()
		if inline.MatchString(body) {
			t.Errorf("%s has inline script/style/handler", path)
		}
		for _, want := range []string{`driga console`, `class="status"`, `class="wins"`, `data-pane`, `id="cmdline"`, `id="command-menu"`, `id="help"`, `/static/app.js`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing %s", path, want)
			}
		}
	}
	body := request(s, "GET", "/", s.token).Body.String()
	for _, want := range []string{`tail -f events`, `aria-live="polite"`, `data-tail`, `data-window="2"`, `value="mocha"`, `value="latte"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
	js := request(s, "GET", "/static/app.js", s.token).Body.String()
	for _, want := range []string{`keydown`, `ArrowDown`, `ArrowUp`, `data-pane`, `data-row`, `parseCommand`, `localStorage`, `EventSource`, `textContent`, `200`} {
		if !strings.Contains(js, want) {
			t.Errorf("keyboard/tail script missing %s", want)
		}
	}
}

func TestConsoleChartBundle(t *testing.T) {
	s, _, _, _ := fixture(t)
	old := "u" + "Plot"
	for _, path := range []string{"/", "/static/app.js", "/static/app.css", "/static/vendor/LICENSES.txt"} {
		if strings.Contains(strings.ToLower(request(s, "GET", path, s.token).Body.String()), strings.ToLower(old)) {
			t.Errorf("legacy chart reference in %s", path)
		}
	}
	entries, _ := assets.ReadDir("static/vendor")
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Name()), strings.ToLower(old)) {
			t.Errorf("legacy asset %s", e.Name())
		}
	}
	w := request(s, "GET", "/static/vendor/echarts-6.1.0.common.min.js", s.token)
	if w.Code != 200 || w.Body.Len() < 100000 {
		t.Fatal("ECharts bundle not served")
	}
	js := request(s, "GET", "/static/app.js", s.token).Body.String()
	for _, want := range []string{"echarts.init", "markLine", "markArea", "dataZoom", "aria", "connectNulls: false", "dispose", "ResizeObserver", "richText"} {
		if !strings.Contains(js, want) {
			t.Errorf("chart wrapper missing %s", want)
		}
	}
}

func TestConsoleCommandParser(t *testing.T) {
	runtime, err := exec.LookPath("osascript")
	if err != nil {
		t.Skip("system JavaScriptCore runner is available on macOS")
	}
	js, _ := assets.ReadFile("static/app.js")
	harness, err := os.ReadFile("testdata/console.js")
	if err != nil {
		t.Fatal("missing command behavior tests:", err)
	}
	code := "eval(" + strconv.Quote(string(js)) + "); eval(" + strconv.Quote(string(harness)) + ");"
	out, err := exec.Command(runtime, "-l", "JavaScript", "-e", code).CombinedOutput()
	if err != nil {
		t.Fatalf("JavaScript behavior: %v\n%s", err, out)
	}
}

func TestConsoleFilters(t *testing.T) {
	s, _, _, _ := fixture(t)
	for _, tc := range []struct {
		path  string
		count int
	}{{"/agents?state=waiting", 1}, {"/agents?state=working", 0}, {"/agents?project=Fixture+project", 1}, {"/agents?project=missing", 0}} {
		p, code, err := s.loadPage(httptest.NewRequest("GET", tc.path, nil))
		if err != nil || code != 200 || len(p.AgentRows) != tc.count {
			t.Errorf("%s: count=%d status=%d err=%v", tc.path, len(p.AgentRows), code, err)
		}
		if !strings.Contains(p.RefreshURL, strings.Split(tc.path, "?")[1]) {
			t.Errorf("refresh loses filter: %s", p.RefreshURL)
		}
	}
	w := request(s, "GET", "/projects?project=Fixture+project", s.token)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "WHERE THINGS STAND") {
		t.Error("where command does not open real project detail")
	}
}

func TestConsoleTailBoundAndEscaping(t *testing.T) {
	s, st, _, _ := fixture(t)
	now := time.Now().Add(-time.Minute)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		for i := 0; i < 240; i++ {
			if err := tx.InsertEvent(model.Event{Adapter: "test", ExternalID: fmt.Sprint(i), TS: now.Add(time.Duration(i) * time.Millisecond), Kind: "Stop", Summary: fmt.Sprintf("line-%03d <script>stored</script>\nnext", i)}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		peer := tx.ForMachine("tail-peer")
		if err := peer.UpsertMachine(model.Machine{Hostname: "Tail peer", State: "live", LastContactAt: time.Now()}); err != nil {
			return err
		}
		return peer.InsertEvent(model.Event{Adapter: "peer", ExternalID: "one", TS: time.Now(), Kind: "Notification", Summary: "peer-only-secret"})
	}); err != nil {
		t.Fatal(err)
	}
	local := request(s, "GET", "/?machine=local", s.token).Body.String()
	if strings.Contains(local, "peer-only-secret") {
		t.Error("tail leaked another machine's event")
	}
	peer := request(s, "GET", "/?machine=tail-peer", s.token).Body.String()
	if !strings.Contains(peer, "peer-only-secret") || strings.Contains(peer, "line-239") {
		t.Error("tail does not respect peer scope")
	}
	body := request(s, "GET", "/", s.token).Body.String()
	if strings.Count(body, `<div data-row data-event-key=`) != 200 {
		t.Error("tail lines are not wired to pane selection/filtering")
	}
	if strings.Count(body, `data-event-key=`) != 200 {
		t.Errorf("initial tail should contain 200 lines, got %d", strings.Count(body, `data-event-key=`))
	}
	if strings.Contains(body, "<script>stored</script>") || !strings.Contains(body, "&lt;script&gt;stored&lt;/script&gt;") {
		t.Error("tail is missing or unescaped")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { r.Host = s.opts.Host; s.ServeHTTP(w, r) }))
	defer ts.Close()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/events", nil)
	req.AddCookie(&http.Cookie{Name: s.cookieName, Value: s.token})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	scan := bufio.NewScanner(resp.Body)
	tail := false
	for scan.Scan() {
		if scan.Text() == "event: tail" {
			tail = true
			continue
		}
		if tail && strings.HasPrefix(scan.Text(), "data: ") {
			payload := strings.TrimPrefix(scan.Text(), "data: ")
			var rows []json.RawMessage
			if err := json.Unmarshal([]byte(payload), &rows); err != nil {
				t.Fatal(err)
			}
			if len(rows) == 0 || len(rows) > 20 {
				t.Fatalf("unbounded or empty tail: %d", len(rows))
			}
			if strings.Contains(payload, "<script>") {
				t.Error("stored markup not JSON escaped")
			}
			first := time.Now()
			if err := st.Tx(context.Background(), func(tx *store.Tx) error {
				for i := 0; i < 60; i++ {
					if err := tx.InsertEvent(model.Event{Adapter: "test", ExternalID: fmt.Sprint("burst", i), TS: time.Now(), Kind: "Notification", Summary: "burst"}); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			tail = false
			for scan.Scan() {
				if scan.Text() == "event: tail" {
					tail = true
					continue
				}
				if tail && strings.HasPrefix(scan.Text(), "data: ") {
					var burst []json.RawMessage
					if err := json.Unmarshal([]byte(strings.TrimPrefix(scan.Text(), "data: ")), &burst); err != nil {
						t.Fatal(err)
					}
					if len(burst) == 0 || len(burst) > 20 {
						t.Fatalf("burst exceeds rate bound: %d", len(burst))
					}
					if time.Since(first) < 900*time.Millisecond {
						t.Error("tail emitted more than one batch per second")
					}
					return
				}
			}
			t.Fatal("no live tail after burst")
			return
		}
	}
	t.Fatalf("no tail payload: %v", scan.Err())
}

func TestConsoleMachineProcessScope(t *testing.T) {
	s, _, _, _ := fixture(t)
	for _, machine := range []string{"local", "peer"} {
		p := pageData{Snapshot: viewer.Snapshot{LocalMachineID: "local", Procs: []model.Proc{{MachineID: "local", PID: 123}}, Machine: view.MachineView{Machines: []view.MachineView{{MachineID: machine, Sample: model.MachineSample{TS: time.Now(), Cores: 4}}}}}}
		var body strings.Builder
		if err := s.templates.ExecuteTemplate(&body, "gauges", p); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(body.String(), "<dt>procs</dt>") != (machine == "local") {
			t.Errorf("%s displays another machine's process count", machine)
		}
	}
}
