package web

import (
	"bufio"
	"context"
	"github.com/labstack/driga/internal/chat"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
func TestChatSSEWaitsForBrokerEvents(t *testing.T) {
	s, b, id := chatFixture(t)
	client := &countingChat{Client: b}
	s.opts.Chat = client
	server := httptest.NewServer(s)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/chat/api/events?id="+id, nil)
	r.Host = s.opts.Host
	r.AddCookie(&http.Cookie{Name: s.cookieName, Value: s.token})
	res, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	scanner := bufio.NewScanner(res.Body)
	next := func() string {
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "event: ") {
				return strings.TrimPrefix(scanner.Text(), "event: ")
			}
		}
		t.Fatal(scanner.Err())
		return ""
	}
	if got := next(); got != "reset" {
		t.Fatal(got)
	}
	if got := next(); got != "status" {
		t.Fatal(got)
	}

	time.Sleep(350 * time.Millisecond)
	if client.reads.Load() != 1 || client.waits.Load() != 1 {
		t.Fatalf("idle reads=%d waits=%d", client.reads.Load(), client.waits.Load())
	}
	t.Logf("350ms idle: reads=%d waits=%d", client.reads.Load(), client.waits.Load())
	if err := b.SetMode(ctx, id, chat.Auto); err != nil {
		t.Fatal(err)
	}
	if got := next(); got != "change" {
		t.Fatal(got)
	}
	cancel()
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
	if w := request(s, "GET", "/chat/api/history", s.token); w.Code != 200 || !strings.Contains(w.Body.String(), row.ID) {
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
	if w := request(s, "GET", "/chat/api/history", s.token); w.Code != 200 || !strings.Contains(w.Body.String(), row.ID) {
		t.Fatal(w.Code, w.Body)
	}
}
