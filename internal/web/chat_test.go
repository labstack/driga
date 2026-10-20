package web

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/chat"
)

type webAgent struct{}

func (webAgent) Run(ctx context.Context, r chat.Request, emit func(chat.Delta) error) error {
	return emit(chat.Delta{Text: `<script>alert("agent")</script> [[session:1]]`})
}
func chatFixture(t *testing.T) (*Server, *chat.Broker, string) {
	t.Helper()
	s, st, _, _ := fixture(t)
	b, err := chat.New(context.Background(), st, webAgent{}, chat.Options{DataDir: t.TempDir(), Snippets: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	s.opts.Chat = b
	c, err := b.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return s, b, c.ID
}
func postChat(s *Server, path, body, origin, csrf, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "http://127.0.0.1:4321"+path, strings.NewReader(body))
	r.Header.Set("Origin", origin)
	r.Header.Set("X-CSRF-Token", csrf)
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.AddCookie(&http.Cookie{Name: s.cookieName, Value: token})
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func TestChatSecurity(t *testing.T) {
	s, _, id := chatFixture(t)
	body := `{"id":"` + id + `","text":"hello","context":{"page":"flow"}}`
	for _, tc := range []struct {
		origin, csrf, token string
		code                int
	}{
		{"http://127.0.0.1:4321", s.csrf, "", 401},
		{"https://evil.test", s.csrf, s.token, 403},
		{"", s.csrf, s.token, 403},
		{"null", s.csrf, s.token, 403},
		{"http://127.0.0.1:4321", "wrong", s.token, 403},
		{"http://127.0.0.1:4321", s.csrf, s.token, 200},
	} {
		if w := postChat(s, "/chat/api/send", body, tc.origin, tc.csrf, tc.token); w.Code != tc.code {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body)
		}
	}
	if w := request(s, "GET", "/chat/api/send", s.token); w.Code != 405 {
		t.Fatal(w.Code)
	}
	if w := request(s, "GET", "/chat", s.token); w.Code != 200 || !strings.Contains(w.Body.String(), "actions arrive in the next release") {
		t.Fatal(w.Code, w.Body)
	}
}
func TestChatSSEReplayAndEscaping(t *testing.T) {
	s, b, id := chatFixture(t)
	if err := b.Send(context.Background(), id, "hello", chat.Context{}); err != nil {
		t.Fatal(err)
	}
	var state chat.State
	for i := 0; i < 100; i++ {
		state, _ = b.Read(context.Background(), id, 0)
		if !state.Active {
			break
		}
		time.Sleep(time.Millisecond)
	}
	server := httptest.NewServer(s)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/chat/api/events?id="+id, nil)
	r.Host = s.opts.Host
	r.AddCookie(&http.Cookie{Name: s.cookieName, Value: s.token})
	r.Header.Set("Last-Event-ID", strconv.FormatUint(state.Cursor-1, 10))
	res, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	scanner := bufio.NewScanner(res.Body)
	var payload string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			payload = strings.TrimPrefix(line, "data: ")
			break
		}
	}
	var event chat.Event
	if err := json.Unmarshal([]byte(payload), &event); err != nil || event.Seq != state.Cursor {
		t.Fatal(payload, err)
	}
	cancel()
	js := request(s, "GET", "/static/chat.js", s.token).Body.String()
	if strings.Contains(js, "innerHTML") || !strings.Contains(js, "textContent") {
		t.Fatal("agent output must use text nodes")
	}
	raw, _ := json.Marshal(state)
	if strings.Contains(string(raw), "<script>") {
		t.Fatal("SSE must JSON-escape HTML")
	}
}

type partialWebAgent struct{ release <-chan struct{} }

func (a partialWebAgent) Run(ctx context.Context, _ chat.Request, emit func(chat.Delta) error) error {
	for _, text := range []string{"First ", "second"} {
		if err := emit(chat.Delta{Text: text}); err != nil {
			return err
		}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-a.release:
		return emit(chat.Delta{Result: json.RawMessage(`{"type":"result"}`)})
	}
}

func TestChatSSEPartialsAndOrderedReplay(t *testing.T) {
	s, st, _, _ := fixture(t)
	release := make(chan struct{})
	b, err := chat.New(context.Background(), st, partialWebAgent{release}, chat.Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	s.opts.Chat = b
	c, err := b.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	initial, err := b.Read(context.Background(), c.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(s)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connect := func(after uint64) *http.Response {
		t.Helper()
		r, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/chat/api/events?id="+c.ID, nil)
		r.Host = s.opts.Host
		r.AddCookie(&http.Cookie{Name: s.cookieName, Value: s.token})
		r.Header.Set("Last-Event-ID", strconv.FormatUint(after, 10))
		res, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != 200 {
			res.Body.Close()
			t.Fatal(res.Status)
		}
		return res
	}
	res := connect(initial.Cursor)
	defer res.Body.Close()
	if err := b.Send(ctx, c.ID, "question", chat.Context{}); err != nil {
		t.Fatal(err)
	}
	scan := bufio.NewScanner(res.Body)
	after := initial.Cursor
	var deltas []chat.Event
	for scan.Scan() {
		if !strings.HasPrefix(scan.Text(), "data: ") {
			continue
		}
		var e chat.Event
		if err := json.Unmarshal([]byte(strings.TrimPrefix(scan.Text(), "data: ")), &e); err != nil {
			t.Fatal(err)
		}
		if e.Seq == 0 {
			continue
		} // Unsequenced status updates.
		if e.Seq != after+1 {
			t.Fatal("unordered SSE", after, e)
		}
		after = e.Seq
		if e.Kind == "delta" {
			deltas = append(deltas, e)
		}
		if len(deltas) == 2 {
			break
		}
	}
	if len(deltas) != 2 {
		t.Fatal("partials buffered", scan.Err())
	}
	state, err := b.Read(ctx, c.ID, 0)
	if err != nil || !state.Active || len(state.Result) > 0 {
		t.Fatal("client text arrived after result", state, err)
	}
	res.Body.Close()
	// Reconnect from the first delta: replay the second delta exactly once.
	replay := connect(deltas[0].Seq)
	defer replay.Body.Close()
	scan = bufio.NewScanner(replay.Body)
	var got chat.Event
	for scan.Scan() {
		if strings.HasPrefix(scan.Text(), "data: ") {
			if err := json.Unmarshal([]byte(strings.TrimPrefix(scan.Text(), "data: ")), &got); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if got.Seq != deltas[1].Seq || got.Kind != "delta" {
		t.Fatal(got, scan.Err())
	}
	raw, _ := json.Marshal(got)
	if !strings.Contains(string(raw), `"text":"second"`) || !strings.Contains(string(raw), `"message_id":`) {
		t.Fatal(string(raw))
	}
	close(release)
}
