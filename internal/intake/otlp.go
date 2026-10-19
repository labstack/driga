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
	Facts             []Fact
	Dropped, Rejected int
}

type value struct {
	String string   `json:"stringValue"`
	Int    string   `json:"intValue"`
	Bool   *bool    `json:"boolValue"`
	Double *float64 `json:"doubleValue"`
}
type attribute struct {
	Key   string `json:"key"`
	Value value  `json:"value"`
}
type resource struct {
	Attributes []attribute `json:"attributes"`
}
type record struct {
	Time       string          `json:"timeUnixNano"`
	Start      string          `json:"startTimeUnixNano"`
	End        string          `json:"endTimeUnixNano"`
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
			if a.Value.Int != "" {
				v = a.Value.Int
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
			b.Rejected++
			return
		}
		name := attrs["event.name"]
		if name == "" {
			name = r.EventName
		}
		ts := r.Time
		if span {
			name = r.Name
			ts = r.End
		}
		if !strings.HasPrefix(name, "claude_code.") {
			b.Rejected++
			return
		}
		name = strings.TrimPrefix(name, "claude_code.")
		n, err := strconv.ParseInt(ts, 10, 64)
		if err != nil || n <= 0 || attrs["session.id"] == "" {
			b.Rejected++
			return
		}
		f := Fact{SessionID: attrs["session.id"], Name: name, TS: time.Unix(0, n).UTC()}
		if len(f.SessionID) > 256 {
			b.Rejected++
			return
		}
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
			start, e := strconv.ParseInt(r.Start, 10, 64)
			if !span || e != nil || start <= 0 || start > n {
				b.Rejected++
				return
			}
			f.Start = time.Unix(0, start).UTC()
			f.Summary = fmt.Sprintf("interval %s to %s", f.Start.Format(time.RFC3339Nano), f.TS.Format(time.RFC3339Nano))
		default:
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

type handler struct {
	sink   func(context.Context, Batch) (int, error)
	slots  chan struct{}
	mu     sync.Mutex
	window time.Time
	count  int
}

// New returns a bounded OTLP handler. The sink must enqueue work for the collector.
func New(sink func(context.Context, Batch) (int, error)) http.Handler {
	return &handler{sink: sink, slots: make(chan struct{}, 16)}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/logs" && r.URL.Path != "/v1/traces" && r.URL.Path != "/v1/metrics" {
		http.NotFound(w, r)
		return
	}
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(405)
		return
	}
	typ, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || typ != "application/json" {
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
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(429)
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
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
			http.Error(w, "invalid gzip", 400)
			return
		}
		defer z.Close()
		reader = z
	default:
		w.WriteHeader(415)
		return
	}
	b, err := Decode(reader, r.URL.Path)
	var sizeErr *http.MaxBytesError
	if errors.Is(err, errOversize) || errors.As(err, &sizeErr) {
		w.WriteHeader(413)
		return
	}
	if err != nil {
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
	json.NewEncoder(w).Encode(map[string]any{"partialSuccess": map[string]any{key: strconv.Itoa(rejected), "errorMessage": "unsupported, invalid, or unmatched records"}})
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
