package chat

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type deadlineTransport func(*http.Request) (*http.Response, error)

func (f deadlineTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRemoteRequestTimeouts(t *testing.T) {
	b, _, _ := fixture(t, true)
	dir := t.TempDir()
	stop, err := Listen(context.Background(), dir, b)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	c, err := dialPath(context.Background(), SocketPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, path := range []string{"/conversations", "/send", "/stop", "/state", "/mode", "/approve", "/hook-prompt"} {
		t.Run(path, func(t *testing.T) {
			want := 10 * time.Second
			if path == "/approve" || path == "/hook-prompt" {
				want = ApprovalTTL + time.Minute
			}
			c.http.Transport = deadlineTransport(func(r *http.Request) (*http.Response, error) {
				deadline, ok := r.Context().Deadline()
				remaining := time.Until(deadline)
				if !ok || remaining > want || remaining < want-time.Second {
					t.Errorf("deadline %v; want %v", remaining, want)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
			})
			if err := c.call(context.Background(), "POST", path, nil, nil); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			c.http.Transport = deadlineTransport(func(r *http.Request) (*http.Response, error) {
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) > time.Second {
					t.Error("lost caller deadline")
				}
				return nil, context.Canceled
			})
			if err := c.call(ctx, "POST", path, nil, nil); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}

// A long poll must outlive the broker's wait, which answers "no change" once
// it ends; a shorter client deadline would turn every quiet poll into an error.
func TestRemoteWaitOutlivesBrokerWait(t *testing.T) {
	c := &Remote{http: &http.Client{}, transport: &http.Transport{}}
	want := brokerWait + 10*time.Second
	c.http.Transport = deadlineTransport(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		remaining := time.Until(deadline)
		if !ok || remaining > want || remaining < want-time.Second {
			t.Errorf("deadline %v; want %v", remaining, want)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})
	if _, err := c.Wait(context.Background(), "id", 3); err != nil {
		t.Fatal(err)
	}
}
