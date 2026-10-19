// Package intake receives local OTLP/HTTP JSON without retaining content.
package intake

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/driga/internal/scrub"
)

const maxBody = 8 << 20

// ErrBusy asks exporters to retry when the collector cannot accept more work.
var ErrBusy = errors.New("intake busy")
var errOversize = errors.New("OTLP body exceeds 8 MiB")

// Fact contains only the metadata needed to annotate an existing Claude session.
type Fact struct {
	SessionID, Name, Source, Summary string
	TS, Start                        time.Time
}

// Batch is sanitized before it reaches the collector's single writer.
type Batch struct {
	Facts                      []Fact
	Dropped, Rejected, Ignored int
}

// OTLP's protobuf JSON mapping accepts both strings and numbers for int64
// and fixed64. Decode the original digits, never via float64 (timestamps lose
// precision there). Null has the protobuf default value.
type int64Value int64
type uint64Value uint64

// Protobuf JSON also permits exponent notation and integral decimal numbers.
// Normalize the digits with bounded string operations, avoiding both float
// rounding and allocations proportional to an attacker-supplied exponent.
func integerDigits(raw []byte) (string, error) {
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return "", errors.New("invalid integer")
	}
	s := n.String()
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var exponent int64
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		var err error
		exponent, err = strconv.ParseInt(s[i+1:], 10, 32)
		if err != nil {
			return "", errors.New("invalid integer")
		}
		s = s[:i]
	}
	if i := strings.IndexByte(s, '.'); i >= 0 {
		exponent -= int64(len(s) - i - 1)
		s = s[:i] + s[i+1:]
	}
	s = strings.TrimLeft(s, "0")
	if s == "" {
		return "0", nil
	}
	if exponent < 0 {
		if exponent < -int64(len(s)) {
			return "", errors.New("fractional integer")
		}
		cut := int64(len(s)) + exponent
		if strings.Trim(s[cut:], "0") != "" {
			return "", errors.New("fractional integer")
		}
		s = s[:cut]
		exponent = 0
	}
	if int64(len(s))+exponent > 20 {
		return "", errors.New("integer out of range")
	}
	s += strings.Repeat("0", int(exponent))
	if negative {
		s = "-" + s
	}
	return s, nil
}

func (v *int64Value) UnmarshalJSON(raw []byte) error {
	if bytes.Equal(raw, []byte("null")) {
		*v = 0
		return nil
	}
	digits, err := integerDigits(raw)
	if err != nil {
		return errors.New("invalid int64")
	}
	x, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return errors.New("invalid int64")
	}
	*v = int64Value(x)
	return nil
}

func (v *uint64Value) UnmarshalJSON(raw []byte) error {
	if bytes.Equal(raw, []byte("null")) {
		*v = 0
		return nil
	}
	digits, err := integerDigits(raw)
	if err != nil {
		return errors.New("invalid fixed64")
	}
	x, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return errors.New("invalid fixed64")
	}
	*v = uint64Value(x)
	return nil
}

type value struct {
	String string      `json:"stringValue"`
	Int    *int64Value `json:"intValue"`
	// Other AnyValue variants (bool/double/bytes/array/kvlist/empty) are not
	// metadata we consume. Leave them unmaterialized, including nested content.
}
type attribute struct {
	Key   string `json:"key"`
	Value value  `json:"value"`
}
type resource struct {
	Attributes []attribute `json:"attributes"`
}
type record struct {
	Time       uint64Value     `json:"timeUnixNano"`
	Observed   uint64Value     `json:"observedTimeUnixNano"`
	Start      uint64Value     `json:"startTimeUnixNano"`
	End        uint64Value     `json:"endTimeUnixNano"`
	Name       string          `json:"name"`
	EventName  string          `json:"eventName"`
	Body       json.RawMessage `json:"body"`
	Attributes []attribute     `json:"attributes"`
}
type document struct {
	Logs []struct {
		Resource resource `json:"resource"`
		Scopes   []struct {
			Records []record `json:"logRecords"`
		} `json:"scopeLogs"`
	} `json:"resourceLogs"`
	Spans []struct {
		Resource resource `json:"resource"`
		Scopes   []struct {
			Records []record `json:"spans"`
		} `json:"scopeSpans"`
	} `json:"resourceSpans"`
}

func attributes(attrs []attribute, dropped *int) map[string]string {
	out := make(map[string]string)
	for _, a := range attrs {
		switch a.Key {
		case "prompt", "prompt_text", "response", "tool_parameters", "tool_input", "body", "body_ref", "organization.id", "vcs.repository.url.full":
			*dropped++
			continue
		}
		if strings.HasPrefix(a.Key, "user.") {
			*dropped++
			continue
		}
		// An allowlist also keeps unknown content-bearing attributes out of facts.
		switch a.Key {
		case "service.name", "session.id", "event.name", "source", "tool_name", "status_code", "attempt", "from_mode", "to_mode", "agent_type", "total_tool_uses", "status":
			v := a.Value.String
			if a.Value.Int != nil {
				v = strconv.FormatInt(int64(*a.Value.Int), 10)
			}
			out[a.Key] = v
		}
	}
	return out
}

// Decode ignores unknown OTLP fields and discards content before producing facts.
func Decode(r io.Reader, path string) (Batch, error) {
	var b Batch
	raw, err := io.ReadAll(io.LimitReader(r, maxBody+1))
	if err != nil {
		return b, err
	}
	if len(raw) > maxBody {
		return b, errOversize
	}
	if len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return b, errors.New("OTLP request must be an object")
	}
	var doc document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return b, errors.New("invalid OTLP JSON")
	}
	add := func(res map[string]string, r record, span bool) {
		attrs := attributes(r.Attributes, &b.Dropped)
		if len(r.Body) > 0 {
			b.Dropped++
		}
		for k, v := range res {
			if _, ok := attrs[k]; !ok {
				attrs[k] = v
			}
		}
		if res["service.name"] != "claude-code" && res["service.name"] != "claude-code-desktop" {
			b.Ignored++
			return
		}
		name := attrs["event.name"]
		if name == "" {
			name = r.EventName
		}
		ts := r.Time
		if ts == 0 {
			ts = r.Observed
		}
		if span {
			name = r.Name
			ts = r.End
		}
		// Claude Code 2.1.303 emits bare event.name values. The service gate
		// above keeps these aliases scoped to Claude; retain prefixed names too.
		name = strings.TrimPrefix(name, "claude_code.")
		f := Fact{SessionID: attrs["session.id"], Name: name, TS: nanoTime(ts)}
		switch name {
		case "user_prompt", "api_request", "assistant_response":
			if span {
				b.Rejected++
				return
			}
		case "tool_decision":
			if span {
				b.Rejected++
				return
			}
			f.Source = attrs["source"]
			switch f.Source {
			case "user_permanent", "user_temporary", "user_abort", "user_reject", "config", "hook":
			default:
				f.Source = ""
			}
			f.Summary = f.Source
		case "tool_result":
			if span {
				b.Rejected++
				return
			}
			f.Summary = scrub.Snippet(attrs["tool_name"])
		case "api_error":
			if span {
				b.Rejected++
				return
			}
			code, _ := strconv.ParseUint(attrs["status_code"], 10, 32)
			attempt, _ := strconv.ParseUint(attrs["attempt"], 10, 32)
			f.Summary = fmt.Sprintf("API error %d, attempt %d", code, attempt)
		case "permission_mode_changed":
			if span {
				b.Rejected++
				return
			}
			f.Summary = "permission mode changed"
		case "subagent_completed":
			if span {
				b.Rejected++
				return
			}
			count, _ := strconv.ParseUint(attrs["total_tool_uses"], 10, 32)
			f.Summary = fmt.Sprintf("worker finished; %d tool uses", count)
		case "mcp_server_connection":
			if span {
				b.Rejected++
				return
			}
			if attrs["status"] == "failed" {
				f.Summary = "connection failed"
			} else {
				f.Summary = "connection status changed"
			}
		case "tool.blocked_on_user", "interaction":
			if !span || r.Start == 0 || r.Start > ts {
				b.Rejected++
				return
			}
			f.Start = nanoTime(r.Start)
			f.Summary = fmt.Sprintf("interval %s to %s", f.Start.Format(time.RFC3339Nano), f.TS.Format(time.RFC3339Nano))
		default:
			b.Ignored++
			return
		}
		if ts == 0 || f.SessionID == "" || len(f.SessionID) > 256 {
			b.Rejected++
			return
		}
		b.Facts = append(b.Facts, f)
	}
	if path == "/v1/traces" {
		for _, rs := range doc.Spans {
			res := attributes(rs.Resource.Attributes, &b.Dropped)
			for _, s := range rs.Scopes {
				for _, r := range s.Records {
					add(res, r, true)
				}
			}
		}
	} else {
		for _, rs := range doc.Logs {
			res := attributes(rs.Resource.Attributes, &b.Dropped)
			for _, s := range rs.Scopes {
				for _, r := range s.Records {
					add(res, r, false)
				}
			}
		}
	}
	return b, nil
}

func nanoTime(n uint64Value) time.Time {
	return time.Unix(int64(n/1_000_000_000), int64(n%1_000_000_000)).UTC()
}

// Stats counts rejected HTTP requests independently of successfully decoded
// batches. Reasons are fixed labels and never include request content.
type Stats struct {
	Rejected, ParseFailures int64
	Reasons                 map[string]int64
}

func RejectionReasons() []string {
	return []string{"path", "method", "content type", "content encoding", "rate limit", "busy", "invalid gzip", "body too large", "invalid json"}
}

type Handler struct {
	sink     func(context.Context, Batch) (int, error)
	slots    chan struct{}
	mu       sync.Mutex
	window   time.Time
	count    int
	stats    Stats
	lastLog  map[string]time.Time
	onReject func()
}

// New returns a bounded OTLP handler. The sink must enqueue work for the collector.
// Optional onReject is a nonblocking notification to persist a fresh Stats snapshot.
func New(sink func(context.Context, Batch) (int, error), onReject ...func()) *Handler {
	h := &Handler{sink: sink, slots: make(chan struct{}, 16), stats: Stats{Reasons: make(map[string]int64)}, lastLog: make(map[string]time.Time)}
	if len(onReject) > 0 {
		h.onReject = onReject[0]
	}
	return h
}

func (h *Handler) Stats() Stats {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := Stats{Rejected: h.stats.Rejected, ParseFailures: h.stats.ParseFailures, Reasons: make(map[string]int64)}
	for k, v := range h.stats.Reasons {
		s.Reasons[k] = v
	}
	return s
}

func (h *Handler) reject(reason string, status int) {
	h.mu.Lock()
	h.stats.Rejected++
	h.stats.Reasons[reason]++
	if reason == "invalid json" {
		h.stats.ParseFailures++
	}
	now := time.Now()
	log := now.Sub(h.lastLog[reason]) >= time.Minute
	if log {
		h.lastLog[reason] = now
	}
	count := h.stats.Reasons[reason]
	h.mu.Unlock()
	if log {
		slog.Warn("OTLP request rejected", "event", "otlp_request_rejected", "reason", reason, "status", status, "count", count)
	}
	if h.onReject != nil {
		h.onReject()
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/logs" && r.URL.Path != "/v1/traces" && r.URL.Path != "/v1/metrics" {
		h.reject("path", 404)
		http.NotFound(w, r)
		return
	}
	if r.Method != "POST" {
		h.reject("method", 405)
		w.Header().Set("Allow", "POST")
		w.WriteHeader(405)
		return
	}
	typ, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || typ != "application/json" {
		h.reject("content type", 415)
		http.Error(w, "OTLP/HTTP JSON required", 415)
		return
	}
	h.mu.Lock()
	now := time.Now()
	if now.Sub(h.window) >= time.Second {
		h.window = now
		h.count = 0
	}
	h.count++
	limited := h.count > 10
	h.mu.Unlock()
	if limited {
		h.reject("rate limit", 429)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(429)
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		h.reject("busy", 503)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(503)
		return
	}
	var reader io.Reader = http.MaxBytesReader(w, r.Body, maxBody)
	switch r.Header.Get("Content-Encoding") {
	case "":
	case "gzip":
		z, e := gzip.NewReader(reader)
		if e != nil {
			h.reject("invalid gzip", 400)
			http.Error(w, "invalid gzip", 400)
			return
		}
		defer z.Close()
		reader = z
	default:
		h.reject("content encoding", 415)
		w.WriteHeader(415)
		return
	}
	b, err := Decode(reader, r.URL.Path)
	var sizeErr *http.MaxBytesError
	if errors.Is(err, errOversize) || errors.As(err, &sizeErr) {
		h.reject("body too large", 413)
		w.WriteHeader(413)
		return
	}
	if err != nil {
		h.reject("invalid json", 400)
		http.Error(w, "invalid OTLP JSON", 400)
		return
	}
	// Metrics are acknowledged and discarded, with the same body bounds.
	if r.URL.Path == "/v1/metrics" {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, "{}")
		return
	}
	rejected := b.Rejected
	if h.sink != nil {
		n, e := h.sink(r.Context(), b)
		if e != nil {
			h.reject("busy", 503)
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(503)
			return
		}
		rejected += n
	}
	w.Header().Set("Content-Type", "application/json")
	if rejected == 0 {
		io.WriteString(w, "{}")
		return
	}
	key := "rejectedLogRecords"
	if r.URL.Path == "/v1/traces" {
		key = "rejectedSpans"
	}
	// OTLP int64 response counters are string encoded too.
	json.NewEncoder(w).Encode(map[string]any{"partialSuccess": map[string]any{key: strconv.Itoa(rejected), "errorMessage": "invalid records"}})
}

// Listen refuses DNS names and every non-loopback bind address.
func Listen(addr string) (net.Listener, error) {
	host, port, err := net.SplitHostPort(addr)
	ip := net.ParseIP(host)
	p, e := strconv.Atoi(port)
	if err != nil || ip == nil || !ip.IsLoopback() || e != nil || p < 0 || p > 65535 {
		return nil, errors.New("OTLP intake requires a loopback IP and numeric port")
	}
	return net.Listen("tcp", addr)
}

type limitedListener struct {
	net.Listener
	slots    chan struct{}
	accepted []time.Time
}
type limitedConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *limitedConn) Close() error { err := c.Conn.Close(); c.once.Do(c.release); return err }
func (l *limitedListener) Accept() (net.Conn, error) {
	for {
		c, e := l.Listener.Accept()
		if e != nil {
			return nil, e
		}
		now := time.Now()
		for len(l.accepted) > 0 && now.Sub(l.accepted[0]) >= time.Second {
			l.accepted = l.accepted[1:]
		}
		if len(l.accepted) >= 10 {
			c.Close()
			continue
		}
		select {
		case l.slots <- struct{}{}:
			l.accepted = append(l.accepted, now)
			return &limitedConn{Conn: c, release: func() { <-l.slots }}, nil
		default:
			c.Close()
		}
	}
}

// Serve stops its listener and active connections when ctx is cancelled.
func Serve(ctx context.Context, ln net.Listener, h http.Handler) error {
	host, _, err := net.SplitHostPort(ln.Addr().String())
	if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
		ln.Close()
		return errors.New("OTLP intake listener must be loopback")
	}
	s := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 16 << 10, BaseContext: func(net.Listener) context.Context { return ctx }}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			s.Close()
		case <-done:
		}
	}()
	err = s.Serve(&limitedListener{Listener: ln, slots: make(chan struct{}, 16)})
	if errors.Is(err, http.ErrServerClosed) || ctx.Err() != nil {
		return nil
	}
	return err
}
