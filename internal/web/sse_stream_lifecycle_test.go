package web

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type pipeResponseWriter struct {
	net.Conn
	header http.Header
}

func (w *pipeResponseWriter) Header() http.Header { return w.header }
func (w *pipeResponseWriter) WriteHeader(int)     {}
func (w *pipeResponseWriter) FlushError() error   { return nil }
func TestSSESlowReaderDeadline(t *testing.T) {
	original, _, _, _ := fixture(t)
	s, err := New(original.source, original.opts)
	if err != nil {
		t.Fatal(err)
	}
	s.sseWriteTimeout = 30 * time.Millisecond
	close(s.ready)
	writer, reader := net.Pipe()
	defer writer.Close()
	defer reader.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("GET", "/events", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() { defer close(done); s.events(&pipeResponseWriter{Conn: writer, header: http.Header{}}, req) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		writer.Close()
		<-done
		t.Fatal("blocked client kept SSE handler alive")
	}
	s.mu.Lock()
	clients := len(s.clients)
	s.mu.Unlock()
	if clients != 0 {
		t.Fatalf("leaked client: %d", clients)
	}
}

type errorFlusher struct{ *httptest.ResponseRecorder }

func (*errorFlusher) FlushError() error { return errors.New("client disconnected") }
func TestSSEDropsFlushFailure(t *testing.T) {
	original, _, _, _ := fixture(t)
	s, err := New(original.source, original.opts)
	if err != nil {
		t.Fatal(err)
	}
	close(s.ready)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.events(&errorFlusher{httptest.NewRecorder()}, httptest.NewRequest("GET", "/events", nil).WithContext(ctx))
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("flush failure did not drop client")
	}
}

func TestSSETicksWithoutCommits(t *testing.T) {
	original, _, _, _ := fixture(t)
	s, err := New(original.source, original.opts)
	if err != nil {
		t.Fatal(err)
	}
	s.watchInterval = 5 * time.Millisecond
	s.refreshInterval = 20 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	watchDone := make(chan struct{})
	go func() { defer close(watchDone); s.Watch(ctx) }()
	defer func() { cancel(); <-watchDone }()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.events(w, r) }))
	defer ts.Close()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	scan := bufio.NewScanner(resp.Body)
	for scan.Scan() {
		if scan.Text() == "event: change" {
			return
		}
	}
	t.Fatalf("no idle refresh: %v", scan.Err())
}
