package web

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/driga/internal/chat"
)

type countingChat struct {
	chat.Client
	reads, waits atomic.Int32
}

func (c *countingChat) Read(ctx context.Context, id string, after uint64) (chat.State, error) {
	c.reads.Add(1)
	return c.Client.Read(ctx, id, after)
}
func (c *countingChat) Wait(ctx context.Context, id string, after uint64) (chat.State, error) {
	c.waits.Add(1)
	return c.Client.Wait(ctx, id, after)
}
func waitChat(t *testing.T, s *Server, id string, after uint64) chatHTMLState {
	t.Helper()
	w := request(s, "GET", fmt.Sprintf("/chat/api/wait?id=%s&after=%d", id, after))
	if w.Code != 200 {
		t.Fatalf("wait: %d %s", w.Code, w.Body)
	}
	var state chatHTMLState
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestChatWaitReturnsNewerSequence(t *testing.T) {
	s, b, id := chatFixture(t)
	client := &countingChat{Client: b}
	s.opts.Chat = client
	s.pollTimeout = time.Hour
	ctx := context.Background()
	initial := waitChat(t, s, id, 0)
	if !initial.Reset || client.waits.Load() != 0 {
		t.Fatalf("first wait must return the whole conversation at once: %+v waits=%d", initial.State, client.waits.Load())
	}
	time.AfterFunc(100*time.Millisecond, func() { _ = b.SetMode(ctx, id, chat.Auto) })
	start := time.Now()
	next := waitChat(t, s, id, initial.Cursor)
	if next.Cursor <= initial.Cursor || next.Reset || len(next.Events) == 0 || next.Events[0].Seq != initial.Cursor+1 {
		t.Fatalf("newer sequence: %+v", next.State)
	}
	if time.Since(start) < 50*time.Millisecond || client.waits.Load() != 1 {
		t.Fatalf("wait returned before the change: %v waits=%d", time.Since(start), client.waits.Load())
	}
}

func TestChatWaitTimesOutWithStatus(t *testing.T) {
	s, b, id := chatFixture(t)
	s.pollTimeout = 50 * time.Millisecond
	if err := b.SetMode(context.Background(), id, chat.Auto); err != nil {
		t.Fatal(err)
	}
	initial := waitChat(t, s, id, 0)
	start := time.Now()
	got := waitChat(t, s, id, initial.Cursor)
	if got.Cursor != initial.Cursor || got.Reset || len(got.Events) != 0 || time.Since(start) > time.Second {
		t.Fatalf("timeout: %+v after %v", got.State, time.Since(start))
	}
	if w := request(s, "GET", "/chat/api/wait?id="+id+"&after=x"); w.Code != 400 {
		t.Errorf("bad cursor: %d", w.Code)
	}
	if w := request(s, "GET", "/chat/api/wait?id=missing&after=0"); w.Code != 404 {
		t.Errorf("missing conversation: %d", w.Code)
	}
}

func TestWebChatSurvivesCollectorBrokerRestart(t *testing.T) {
	s, st, _, _ := fixture(t)
	ctx := context.Background()
	dir := t.TempDir()
	first, err := chat.New(ctx, st, webAgent{}, chat.Options{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	stop, err := chat.Listen(ctx, dir, first)
	if err != nil {
		first.Close()
		t.Fatal(err)
	}
	t.Cleanup(first.Close)
	t.Cleanup(stop)
	row, err := first.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := chat.Dial(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	s.opts.Chat = remote
	if w := request(s, "GET", "/chat/api/history"); w.Code != 200 || !strings.Contains(w.Body.String(), row.ID) {
		t.Fatal(w.Code, w.Body)
	}
	first.Close()
	stop()
	second, err := chat.New(ctx, st, webAgent{}, chat.Options{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	stop, err = chat.Listen(ctx, dir, second)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if w := request(s, "GET", "/chat/api/history"); w.Code != 200 || !strings.Contains(w.Body.String(), row.ID) {
		t.Fatal(w.Code, w.Body)
	}
}
