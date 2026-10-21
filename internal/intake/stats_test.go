package intake

import (
	"bytes"
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestOTLPRejectionStatsAndLogging(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	changes := 0
	h := New(nil, func() { changes++ })
	for i := 0; i < 2; i++ {
		r := httptest.NewRequest("POST", "/v1/logs", strings.NewReader(`{"NEVER_STORE":`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	s := h.Stats()
	if s.Rejected != 2 || s.ParseFailures != 2 || s.Reasons["invalid json"] != 2 || changes != 2 {
		t.Fatal(s, changes)
	}
	s.Reasons["invalid json"] = 99
	if h.Stats().Reasons["invalid json"] != 2 {
		t.Fatal("mutable stats snapshot")
	}
	if strings.Count(logs.String(), `"event":"otlp_request_rejected"`) != 1 || strings.Contains(logs.String(), "NEVER_STORE") {
		t.Fatal(logs.String())
	}

	r := httptest.NewRequest("POST", "/v1/logs", strings.NewReader("bad gzip"))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Content-Encoding", "gzip")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if h.Stats().Reasons["invalid gzip"] != 1 || strings.Count(logs.String(), `"event":"otlp_request_rejected"`) != 2 {
		t.Fatal(h.Stats(), logs.String())
	}
}

func TestOTLPRejectionReasons(t *testing.T) {
	for _, tt := range []struct {
		method, path, typ, encoding, body, reason string
		status                                    int
	}{
		{"POST", "/missing", "application/json", "", `{}`, "path", 404},
		{"GET", "/v1/logs", "application/json", "", `{}`, "method", 405},
		{"POST", "/v1/logs", "application/x-protobuf", "", `{}`, "content type", 415},
		{"POST", "/v1/logs", "application/json", "br", `{}`, "content encoding", 415},
		{"POST", "/v1/logs", "application/json", "gzip", `bad`, "invalid gzip", 400},
		{"POST", "/v1/logs", "application/json", "", strings.Repeat(" ", maxBody+1), "body too large", 413},
	} {
		t.Run(tt.reason, func(t *testing.T) {
			h := New(nil)
			r := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			r.Header.Set("Content-Type", tt.typ)
			r.Header.Set("Content-Encoding", tt.encoding)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.status || h.Stats().Rejected != 1 || h.Stats().Reasons[tt.reason] != 1 {
				t.Fatal(w.Code, h.Stats())
			}
		})
	}
	h := New(func(context.Context, Batch) (int, error) { return 0, ErrBusy })
	r := httptest.NewRequest("POST", "/v1/logs", bytes.NewReader(fixture(t)))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 503 || h.Stats().Reasons["busy"] != 1 {
		t.Fatal(w.Code, h.Stats())
	}
}

func TestOTLPConcurrentRejections(t *testing.T) {
	h := New(nil)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("POST", "/v1/logs", strings.NewReader("{"))
			r.Header.Set("Content-Type", "application/json")
			h.ServeHTTP(httptest.NewRecorder(), r)
			_ = h.Stats()
		}()
	}
	wg.Wait()
	s := h.Stats()
	var total int64
	for _, n := range s.Reasons {
		total += n
	}
	if s.Rejected != 32 || total != 32 || s.ParseFailures == 0 || s.Reasons["rate limit"] == 0 {
		t.Fatal(s)
	}
}
