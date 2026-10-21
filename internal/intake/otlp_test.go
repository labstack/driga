package intake

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	b, e := os.ReadFile("testdata/claude/events.json")
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func request(t *testing.T, body []byte, typ, enc string, sink func(context.Context, Batch) (int, error)) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "/v1/logs", bytes.NewReader(body))
	r.Header.Set("Content-Type", typ)
	r.Header.Set("Content-Encoding", enc)
	w := httptest.NewRecorder()
	New(sink).ServeHTTP(w, r)
	return w
}
func TestOTLPJSONAccepted(t *testing.T) {
	var got Batch
	w := request(t, fixture(t), "application/json", "", func(_ context.Context, b Batch) (int, error) { got = b; return 0, nil })
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "{}" || len(got.Facts) != 2 || got.Facts[0].Name != "tool_decision" || got.Facts[0].TS.UnixNano() != 1792324800000000000 {
		t.Fatalf("%d %s %+v", w.Code, w.Body, got)
	}
}
func TestOTLPGzip(t *testing.T) {
	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	z.Write(fixture(t))
	z.Close()
	w := request(t, b.Bytes(), "application/json", "gzip", nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
}
func TestOTLPProtobuf415(t *testing.T) {
	w := request(t, []byte{1, 2}, "application/x-protobuf", "", nil)
	if w.Code != 415 {
		t.Fatal(w.Code)
	}
}
func TestOTLPOversize413(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		b := []byte(strings.Repeat(" ", 8*1024*1024+1))
		enc := ""
		if compressed {
			var out bytes.Buffer
			z := gzip.NewWriter(&out)
			z.Write(b)
			z.Close()
			b = out.Bytes()
			enc = "gzip"
		}
		w := request(t, b, "application/json", enc, nil)
		if w.Code != 413 {
			t.Fatal(compressed, w.Code)
		}
	}
}
func TestOTLPPartialSuccess(t *testing.T) {
	w := request(t, fixture(t), "application/json", "", func(context.Context, Batch) (int, error) { return 1, nil })
	var v struct {
		Partial struct {
			Rejected string `json:"rejectedLogRecords"`
		} `json:"partialSuccess"`
	}
	json.Unmarshal(w.Body.Bytes(), &v)
	if w.Code != 200 || v.Partial.Rejected != "1" {
		t.Fatal(w.Code, w.Body)
	}
}
func TestDoorDropsContentAttributes(t *testing.T) {
	b, e := Decode(bytes.NewReader(fixture(t)), "/v1/logs")
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(b)
	if b.Dropped != 6 || bytes.Contains(raw, []byte("NEVER_STORE")) {
		t.Fatalf("%s dropped=%d", raw, b.Dropped)
	}
}
func TestIntakeLoopbackOnly(t *testing.T) {
	for _, a := range []string{"0.0.0.0:0", "[::]:0", "192.0.2.1:0", "localhost:0"} {
		if ln, e := Listen(a); e == nil {
			ln.Close()
			t.Fatal(a)
		}
	}
	ln, e := Listen("127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	ln.Close()
}
func TestOTLPMalformedAndBackpressure(t *testing.T) {
	for _, b := range []string{"{", "{} {}", "null", "[]"} {
		w := request(t, []byte(b), "application/json", "", nil)
		if w.Code != 400 {
			t.Fatal(b, w.Code)
		}
	}
	w := request(t, fixture(t), "application/json", "", func(context.Context, Batch) (int, error) { return 0, ErrBusy })
	if w.Code != 503 || w.Header().Get("Retry-After") == "" {
		t.Fatal(w.Code, w.Header())
	}
}
func FuzzOTLPDecoder(f *testing.F) {
	b, _ := os.ReadFile("testdata/claude/events.json")
	f.Add(b)
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, b []byte) { Decode(bytes.NewReader(b), "/v1/logs") })
}

func TestOTLPTracesMetricsAndUnmappedCodex(t *testing.T) {
	b, err := os.ReadFile("testdata/claude/spans.json")
	if err != nil {
		t.Fatal(err)
	}
	batch, err := Decode(bytes.NewReader(b), "/v1/traces")
	if err != nil || len(batch.Facts) != 1 || batch.Facts[0].Name != "tool.blocked_on_user" || batch.Facts[0].TS.Sub(batch.Facts[0].Start).Seconds() != 3 {
		t.Fatal(batch, err)
	}
	for _, tt := range []struct{ path, body string }{{"/v1/metrics", `{"resourceMetrics":[]}`}, {"/v1/logs", strings.ReplaceAll(string(fixture(t)), "claude-code", "codex")}} {
		called := false
		h := New(func(_ context.Context, b Batch) (int, error) {
			called = true
			if len(b.Facts) > 0 {
				t.Fatal("Codex mapped", b)
			}
			return 0, nil
		})
		r := httptest.NewRequest("POST", tt.path, strings.NewReader(tt.body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || (tt.path == "/v1/metrics" && called) {
			t.Fatal(w.Code, w.Body, called)
		}
	}
	r := httptest.NewRequest("POST", "/v1/traces", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	New(func(context.Context, Batch) (int, error) { return 1, nil }).ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `"rejectedSpans":"1"`) {
		t.Fatal(w.Body)
	}
}

func TestOTLPRateLimit(t *testing.T) {
	h := New(nil)
	for i := 0; i < 11; i++ {
		r := httptest.NewRequest("POST", "/v1/logs", strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := 200
		if i == 10 {
			want = 429
		}
		if w.Code != want {
			t.Fatal(i, w.Code)
		}
	}
}
