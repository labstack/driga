package fleet

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/labstack/driga/internal/store"
	"golang.org/x/sync/semaphore"
)

var errPresence = errors.New("viewer presence changed")
var errStop = errors.New("peer stopped stream")

// Puller follows one pinned peer. It never opens or writes a database.
type Puller struct {
	peer       Peer
	id         Identity
	out        chan<- Batch
	cursor     Cursor
	client     *http.Client
	transport  *http.Transport
	initErr    error
	presence   atomic.Bool
	changed    chan struct{}
	now        func() time.Time
	contacts   uint64
	skewMS     *int64
	resnapshot bool
	ackMu      sync.Mutex
	acks       map[string]bool
	ackChanged chan struct{}
	ackEpoch   atomic.Uint64
}

// NewPuller constructs a peer client without opening a network connection.
func NewPuller(peer Peer, id Identity, out chan<- Batch) *Puller {
	p := &Puller{peer: peer, id: id, out: out, changed: make(chan struct{}, 1), ackChanged: make(chan struct{}, 1), now: time.Now}
	pin, err := ParseDeviceID(peer.DeviceID)
	p.initErr = err
	cfg := PinnedTLS(id, func(fp [32]byte) bool { return err == nil && EqualFingerprint(pin, fp) })
	p.transport = &http.Transport{TLSClientConfig: cfg, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, MaxResponseHeaderBytes: 32 << 10, MaxIdleConnsPerHost: 1}
	p.client = &http.Client{Transport: p.transport, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("fleet redirects are refused") }}
	return p
}

// Resume supplies the last committed cursor before Run starts.
func (p *Puller) Resume(cursor Cursor) { p.cursor = cursor }

// SetPresence reconnects with the new watch state and the committed cursor.
func (p *Puller) SetPresence(watched bool) {
	if p.presence.Swap(watched) == watched {
		return
	}
	select {
	case p.changed <- struct{}{}:
	default:
	}
}

func (p *Puller) emit(ctx context.Context, b Batch) error {
	b.MachineID = p.peer.MachineID
	b.Peer = &p.peer
	b.done = make(chan error, 1)
	select {
	case p.out <- b:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-b.done:
		if err == nil && (!b.ContactAt.IsZero() || b.Snapshot != nil) {
			p.contacts++
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (p *Puller) status(ctx context.Context, state string, err error) {
	message := ""
	if err != nil {
		message = err.Error()
	}
	_ = p.emit(ctx, Batch{State: state, Error: message, Disconnected: state == ""})
}

// Run snapshots once, resumes committed positions on reconnect, and stops on
// cancellation or HTTP 204. Retry delays grow from two to sixty seconds.
func (p *Puller) Run(ctx context.Context) {
	defer p.transport.CloseIdleConnections()
	if p.initErr != nil || p.peer.State == "untrusted" || p.peer.MachineID == "" || p.peer.MachineID == p.id.Cert.Leaf.Subject.CommonName {
		p.status(ctx, "untrusted", fmt.Errorf("invalid peer identity; run driga fleet rekey on the peer, then remove and pair again"))
		return
	}
	ackCtx, stopAcks := context.WithCancel(ctx)
	ackDone := make(chan struct{})
	go func() { defer close(ackDone); p.runAcks(ackCtx) }()
	defer func() { stopAcks(); <-ackDone }()
	delay := 2 * time.Second
	for ctx.Err() == nil {
		contacts := p.contacts
		err := p.connect(ctx)
		if p.contacts != contacts {
			delay = 2 * time.Second
		}
		if ctx.Err() != nil || errors.Is(err, errStop) {
			return
		}
		if errors.Is(err, errPresence) {
			continue
		}
		if strings.Contains(err.Error(), "untrusted fleet peer certificate") || strings.Contains(err.Error(), "machine_id mismatch") {
			p.status(ctx, "untrusted", fmt.Errorf("peer identity changed; run driga fleet rekey on the peer (pairings must be redone), then driga fleet remove and pair again: %w", err))
			return
		}
		var incompatible *schemaError
		if errors.As(err, &incompatible) {
			p.status(ctx, "incompatible", err)
			return
		}
		p.status(ctx, "", err)
		wait := min(60*time.Second, time.Duration(float64(delay)*(0.8+rand.Float64()*0.4)))
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-p.changed:
			timer.Stop()
		case <-timer.C:
		}
		delay = min(60*time.Second, delay*2)
	}
}

type schemaError struct{ schema int }

func (e *schemaError) Error() string { return fmt.Sprintf("incompatible (schema %d)", e.schema) }

type hello struct {
	Schema     int       `json:"schema"`
	MachineID  string    `json:"machine_id"`
	FeedEpoch  string    `json:"feed_epoch"`
	FeedSeq    int64     `json:"feed_seq"`
	ObservedAt time.Time `json:"observed_at"`
}

func (p *Puller) get(ctx context.Context, path string, cursor Cursor) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://"+p.peer.Addr+path, nil)
	if err != nil {
		return nil, err
	}
	if cursor.Epoch != "" {
		req.Header.Set("Last-Event-ID", cursor.Epoch+":"+strconv.FormatInt(cursor.Seq, 10))
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.TLS == nil || len(resp.TLS.PeerCertificates) == 0 || resp.TLS.PeerCertificates[0].Subject.CommonName != p.peer.MachineID {
		resp.Body.Close()
		return nil, fmt.Errorf("peer machine_id mismatch; run driga fleet rekey on the peer (pairings must be redone)")
	}
	if resp.StatusCode == http.StatusNoContent {
		resp.Body.Close()
		return nil, errStop
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("fleet %s: %s", path, resp.Status)
	}
	return resp, nil
}
func decodeBody(resp *http.Response, limit int64, out any) error {
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if int64(len(b)) > limit {
		if limit == 64<<20 {
			return fmt.Errorf("snapshot too large")
		}
		return fmt.Errorf("fleet response too large")
	}
	return json.Unmarshal(b, out)
}
func (p *Puller) connect(ctx context.Context) error {
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	started := p.now()
	resp, err := p.get(requestCtx, "/v1/hello", Cursor{})
	if err != nil {
		cancel()
		return err
	}
	var h hello
	err = decodeBody(resp, 8<<20, &h)
	cancel()
	if err != nil {
		return err
	}
	received := p.now()
	if !h.ObservedAt.IsZero() {
		skew := h.ObservedAt.Sub(started.Add(received.Sub(started) / 2)).Milliseconds()
		p.skewMS = &skew
	}
	if h.MachineID != p.peer.MachineID {
		return fmt.Errorf("hello machine_id mismatch; run driga fleet rekey on the peer (pairings must be redone)")
	}
	if h.Schema != 1 {
		return &schemaError{h.Schema}
	}
	p.ackEpoch.Add(1)
	p.notifyAcks()
	for ctx.Err() == nil {
		if p.cursor.Epoch == "" {
			requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			resp, err := p.get(requestCtx, "/v1/snapshot", Cursor{})
			if err != nil {
				cancel()
				return err
			}
			var snap Snapshot
			err = decodeBody(resp, 64<<20, &snap)
			cancel()
			if err != nil {
				return err
			}
			if snap.Schema != 1 {
				return &schemaError{snap.Schema}
			}
			if err = p.emit(ctx, Batch{Snapshot: &snap, Cursor: snap.Data.Cursor, ContactAt: p.now(), ClockSkewMS: p.skewMS, Resnapshot: p.resnapshot}); err != nil {
				return err
			}
			p.cursor = snap.Data.Cursor
			p.resnapshot = false
		}
		err = p.follow(ctx)
		if errors.Is(err, store.ErrResnapshot) {
			p.cursor = Cursor{}
			p.resnapshot = true
			continue
		}
		return err
	}
	return ctx.Err()
}

func (p *Puller) follow(parent context.Context) (result error) {
	// Coalesce changes before starting; a change after this point cancels the
	// request, including one stalled before its response headers arrive.
	select {
	case <-p.changed:
	default:
	}
	watched := p.presence.Load()
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)
	defer func() {
		if errors.Is(context.Cause(ctx), errPresence) {
			result = errPresence
		}
	}()
	monitorDone := make(chan struct{})
	defer close(monitorDone)
	go func() {
		select {
		case <-p.changed:
			cancel(errPresence)
		case <-monitorDone:
		case <-ctx.Done():
		}
	}()
	path := "/v1/events"
	if watched {
		path += "?presence=1"
	}
	resp, err := p.get(ctx, path, p.cursor)
	if err != nil {
		if parent.Err() == nil && ctx.Err() != nil {
			return errPresence
		}
		return err
	}
	defer resp.Body.Close()
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return fmt.Errorf("expected event stream")
	}
	idle := time.AfterFunc(45*time.Second, func() { cancel(fmt.Errorf("peer event/ping timeout")) })
	defer idle.Stop()
	// Read independently of writer acknowledgments so a collector wake can
	// drain the available burst into one atomic apply. Bound queued payloads
	// as well as frame count to keep large frames from multiplying memory.
	frames := make(chan pullFrame, streamBuffer)
	budget := semaphore.NewWeighted(8 << 20)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		readPullFrames(ctx, resp.Body, frames, budget)
	}()
	defer func() { cancel(nil); resp.Body.Close(); <-readerDone }()
	for {
		var frame pullFrame
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case frame = <-frames:
		}
		cursor := p.cursor
		var rows []FeedRow
		var terminal error
		var weight int64
		contact := false
	drain:
		for {
			weight += frame.weight
			if frame.err != nil {
				terminal = frame.err
				break
			}
			contact = true
			idle.Reset(45 * time.Second)
			if frame.row != nil {
				if frame.cursor.Epoch != cursor.Epoch {
					terminal = store.ErrResnapshot
					break
				}
				row := *frame.row
				if row.Seq > cursor.Seq {
					if row.Seq != cursor.Seq+1 {
						terminal = store.ErrResnapshot
						break
					}
					rows = append(rows, row)
					cursor = frame.cursor
				}
			}
			if len(rows) == streamBuffer {
				break
			}
			select {
			case frame = <-frames:
			default:
				break drain
			}
		}
		if contact {
			err = p.emit(ctx, Batch{Rows: rows, Cursor: cursor, ContactAt: p.now()})
			if weight > 0 {
				budget.Release(weight)
			}
			if err != nil {
				return err
			}
			p.cursor = cursor
		} else if weight > 0 {
			budget.Release(weight)
		}
		if terminal != nil {
			return terminal
		}
	}
}

type pullFrame struct {
	row    *FeedRow
	cursor Cursor
	err    error
	weight int64
}

// readPullFrames parses wire frames without advancing the committed cursor.
func readPullFrames(ctx context.Context, body io.Reader, out chan<- pullFrame, budget *semaphore.Weighted) {
	send := func(frame pullFrame) bool {
		if frame.weight > 0 {
			if err := budget.Acquire(ctx, frame.weight); err != nil {
				return false
			}
		}
		select {
		case out <- frame:
			return true
		case <-ctx.Done():
			if frame.weight > 0 {
				budget.Release(frame.weight)
			}
			return false
		}
	}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), (8<<20)+1024)
	var id, kind string
	var data strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 0 && line[0] == ':' {
			if strings.TrimSpace(line[1:]) == "ping" && !send(pullFrame{}) {
				return
			}
			continue
		}
		if line == "" {
			if kind == "resnapshot" {
				send(pullFrame{err: store.ErrResnapshot})
				return
			}
			if data.Len() > 0 {
				cursor, err := parseCursor(id)
				if err != nil {
					send(pullFrame{err: err})
					return
				}
				var row FeedRow
				if err := json.Unmarshal([]byte(data.String()), &row); err != nil {
					send(pullFrame{err: err})
					return
				}
				if row.Seq != cursor.Seq || row.Kind != kind {
					send(pullFrame{err: fmt.Errorf("SSE frame does not match its row")})
					return
				}
				if !send(pullFrame{row: &row, cursor: cursor, weight: int64(data.Len())}) {
					return
				}
			}
			id, kind = "", ""
			data.Reset()
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "id":
			id = value
		case "event":
			kind = value
		case "data":
			if data.Len()+len(value)+1 > 8<<20 {
				send(pullFrame{err: fmt.Errorf("SSE frame too large")})
				return
			}
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(value)
		}
	}
	err := scanner.Err()
	if err == nil {
		err = io.ErrUnexpectedEOF
	}
	send(pullFrame{err: err})
}
