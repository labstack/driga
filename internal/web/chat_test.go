package web

import (
	"context"
	"encoding/json"
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
func postChat(s *Server, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func TestChatPOST(t *testing.T) {
	s, _, id := chatFixture(t)
	body := `{"id":"` + id + `","text":"hello","context":{"page":"flow"}}`
	if w := postChat(s, "/chat/api/send", body); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	r := httptest.NewRequest("POST", "/chat/api/send", strings.NewReader(body))
	r.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 415 {
		t.Fatal(w.Code)
	}
	if w := request(s, "GET", "/chat/api/send"); w.Code != 405 {
		t.Fatal(w.Code)
	}
	if w := request(s, "GET", "/chat"); w.Code != 200 || !strings.Contains(w.Body.String(), `id="chat-mode-confirm"`) || strings.Contains(w.Body.String(), "actions arrive in the next release") {
		t.Fatal(w.Code, w.Body)
	}
}
func TestChatWaitReplayAndEscaping(t *testing.T) {
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
	w := request(s, "GET", "/chat/api/wait?id="+id+"&after="+strconv.FormatUint(state.Cursor-1, 10))
	var got chatHTMLState
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || len(got.Events) != 1 || got.Events[0].Seq != state.Cursor {
		t.Fatal(w.Body, err)
	}
	if strings.Contains(w.Body.String(), "<script>") {
		t.Fatal("wait must JSON-escape HTML")
	}
	js := request(s, "GET", "/static/chat.js").Body.String()
	if !strings.Contains(js, "body.innerHTML=m.html") || strings.Contains(js, "function markdown(") || strings.Contains(js, "function inline(") {
		t.Fatal("assistant HTML must come from the server renderer")
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

func TestChatWaitPartialsAndOrderedReplay(t *testing.T) {
	s, st, _, _ := fixture(t)
	release := make(chan struct{})
	b, err := chat.New(context.Background(), st, partialWebAgent{release}, chat.Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	s.opts.Chat = b
	s.pollTimeout = 5 * time.Second
	c, err := b.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	initial, err := b.Read(context.Background(), c.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Send(context.Background(), c.ID, "question", chat.Context{}); err != nil {
		t.Fatal(err)
	}
	after := initial.Cursor
	var deltas []chatHTMLEvent
	for len(deltas) < 2 {
		got := waitChat(t, s, c.ID, after)
		if got.Reset || got.Cursor == after {
			t.Fatal("partials buffered", got.State)
		}
		for _, e := range got.Events {
			if e.Seq != after+1 {
				t.Fatal("unordered events", after, e)
			}
			after = e.Seq
			if e.Kind == "delta" {
				deltas = append(deltas, e)
				if e.HTML != "" {
					t.Fatalf("stream included Markdown HTML: %q", e.HTML)
				}
			}
		}
	}
	state, err := b.Read(context.Background(), c.ID, 0)
	if err != nil || !state.Active || len(state.Result) > 0 {
		t.Fatal("client text arrived after result", state, err)
	}
	// Resume from the first delta: replay the second delta exactly once.
	replay := waitChat(t, s, c.ID, deltas[0].Seq)
	if len(replay.Events) == 0 {
		t.Fatal("no replay")
	}
	got := replay.Events[0]
	if got.Seq != deltas[1].Seq || got.Kind != "delta" || got.HTML != "" {
		t.Fatal(got)
	}
	raw, _ := json.Marshal(got)
	if !strings.Contains(string(raw), `"text":"second"`) || !strings.Contains(string(raw), `"message_id":`) {
		t.Fatal(string(raw))
	}
	close(release)
}
