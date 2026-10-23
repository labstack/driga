package chat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteRefreshesRotatedKeyAndReportsHTTPError(t *testing.T) {
	key := filepath.Join(t.TempDir(), "chat.key")
	if err := os.WriteFile(key, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	var posts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "new" {
			http.Error(w, "invalid client token", 403)
			return
		}
		if r.URL.Path == "/error" {
			http.Error(w, "broker unavailable", 503)
			return
		}
		posts++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	}))
	defer server.Close()
	c := &Remote{token: "old", keyPath: key, http: server.Client()}
	// Route synthetic IPC requests to a local test server; no vendor process.
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r.URL.Scheme = "http"
		r.URL.Host = strings.TrimPrefix(server.URL, "http://")
		return http.DefaultTransport.RoundTrip(r)
	})
	if _, err := c.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if posts != 1 || c.token != "new" {
		t.Fatal(posts, c.token)
	}
	err := c.call(context.Background(), "GET", "/error", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 503: broker unavailable") {
		t.Fatal(err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
