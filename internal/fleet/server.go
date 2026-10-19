package fleet

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
)

const streamBuffer = 1000

type ticker interface {
	Chan() <-chan time.Time
	Stop()
}
type realTicker struct{ *time.Ticker }

func (t realTicker) Chan() <-chan time.Time { return t.C }

// Server serves the local collector's feed over pinned mutual TLS. Its store
// must be a dedicated read-only handle; acknowledgments go through the spool.
type Server struct {
	st      *store.Store
	id      Identity
	dataDir string
	cfg     config.Config
	// SpoolDir is the collector's spool destination (defaults to config.SpoolDir).
	SpoolDir      string
	now           func() time.Time
	newTicker     func(time.Duration) ticker
	lastHeartbeat time.Time
	mu            sync.Mutex
	changed       chan struct{}
	revoked       map[string]bool
	streams       map[*streamState]struct{}
	conns         map[net.Conn]string
	enrollUntil   time.Time
	enrollment    *enrollment
	purges        chan purgeRequest
	resnapshots   uint64
	snapshots     chan struct{}
}
type streamState struct {
	device   string
	cancel   context.CancelFunc
	presence bool
}

// NewServer constructs a server without opening any listener.
func NewServer(st *store.Store, id Identity, dataDir string, cfg config.Config) *Server {
	return &Server{st: st, id: id, dataDir: dataDir, cfg: cfg, SpoolDir: config.SpoolDir(), now: time.Now, newTicker: func(d time.Duration) ticker { return realTicker{time.NewTicker(d)} }, changed: make(chan struct{}), revoked: make(map[string]bool), streams: make(map[*streamState]struct{}), conns: make(map[net.Conn]string), purges: make(chan purgeRequest, 1), snapshots: make(chan struct{}, 2)}
}
func (s *Server) share() feed.ShareText {
	return feed.EffectiveShare(feed.ShareText(s.cfg.Fleet.ShareText), s.cfg.CaptureText)
}

// Notify wakes followers after a committed collector transaction. Broadcasts
// coalesce; each follower rereads the durable feed from its own queued cursor.
func (s *Server) Notify() {
	s.mu.Lock()
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
}
func (s *Server) watch() <-chan struct{} { s.mu.Lock(); defer s.mu.Unlock(); return s.changed }

func (s *Server) viewer(fp [32]byte) (Viewer, bool) {
	viewers, err := LoadViewers(filepath.Join(s.dataDir, "fleet"))
	if err != nil {
		return Viewer{}, false
	}
	for _, v := range viewers {
		pin, err := ParseDeviceID(v.DeviceID)
		if err != nil || !EqualFingerprint(pin, fp) {
			continue
		}
		s.mu.Lock()
		revoked := s.revoked[v.DeviceID]
		s.mu.Unlock()
		return v, !revoked
	}
	return Viewer{}, false
}
func (s *Server) enrollmentOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireEnrollmentLocked()
	return s.now().Before(s.enrollUntil)
}

// Revoke removes a viewer's trust and immediately cancels its streams and TLS
// connections, including idle keep-alive connections. A failed disk write still
// fails closed in this process; the control response reports persistence errors.
func (s *Server) Revoke(deviceID string) { _ = s.revoke(deviceID) }
func (s *Server) revoke(deviceID string) error {
	fp, err := ParseDeviceID(deviceID)
	if err != nil {
		return err
	}
	deviceID = canonicalDeviceID(fp)
	s.mu.Lock()
	s.revoked[deviceID] = true
	var conns []net.Conn
	for stream := range s.streams {
		if stream.device == deviceID {
			stream.cancel()
		}
	}
	for c, id := range s.conns {
		if id == deviceID {
			conns = append(conns, c)
		}
	}
	s.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
	trustMu.Lock()
	defer trustMu.Unlock()
	dir := filepath.Join(s.dataDir, "fleet")
	viewers, err := LoadViewers(dir)
	if err != nil {
		return err
	}
	keep := make([]Viewer, 0, len(viewers))
	for _, v := range viewers {
		if v.DeviceID != deviceID {
			keep = append(keep, v)
		}
	}
	return SaveViewers(dir, keep)
}

// Serve runs HTTPS and the local control socket until cancellation. The caller
// owns the store; Serve owns and closes the supplied listener.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	defer ln.Close()
	if s.id.Cert.Leaf.Subject.CommonName != s.st.MachineID() {
		return fmt.Errorf("identity belongs to machine %s; run `driga fleet rekey` (pairings must be redone)", s.id.Cert.Leaf.Subject.CommonName)
	}
	fleetDir := filepath.Join(s.dataDir, "fleet")
	if err := privateDir(fleetDir); err != nil {
		return err
	}
	info, err := os.Lstat(filepath.Join(fleetDir, "identity.key"))
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0044 != 0 {
		return fmt.Errorf("fleet identity key is group/world readable; require mode 0600")
	}
	if err := atomicPrivateWrite(filepath.Join(fleetDir, "listen"), []byte(ln.Addr().String()+"\n")); err != nil {
		return err
	}
	defer os.Remove(filepath.Join(fleetDir, "listen"))
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	control, err := s.listenControl(runCtx)
	if err != nil {
		return err
	}
	defer control.Close()
	limited := &limitedListener{Listener: ln, admission: newAdmission(s.now)}
	tlsConfig := PinnedTLS(s.id, func(fp [32]byte) bool { _, ok := s.viewer(fp); return ok || s.enrollmentAccepts(fp) })
	tlsConfig.NextProtos = []string{"http/1.1"}
	srv := &http.Server{Handler: s, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, IdleTimeout: confirmTimeout + 5*time.Second, MaxHeaderBytes: 32 << 10, BaseContext: func(net.Listener) context.Context { return runCtx }, ConnState: func(c net.Conn, state http.ConnState) {
		s.mu.Lock()
		defer s.mu.Unlock()
		switch state {
		case http.StateNew:
			s.conns[c] = ""
		case http.StateActive:
			if tc, ok := c.(*tls.Conn); ok {
				cs := tc.ConnectionState()
				if len(cs.PeerCertificates) > 0 {
					s.conns[c] = DeviceID(cs.PeerCertificates[0].Raw)
				}
			}
		case http.StateClosed, http.StateHijacked:
			delete(s.conns, c)
		}
	}}
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-runCtx.Done():
			_ = srv.Close()
		case <-stopped:
		}
	}()
	err = srv.Serve(tls.NewListener(limited, tlsConfig))
	cancel()
	if errors.Is(err, http.ErrServerClosed) || ctx.Err() != nil {
		return nil
	}
	return err
}

// ServeHTTP enforces trust again for every request so enrollment never grants
// data access and revocation also covers previously authenticated connections.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	if r.Method == http.MethodPost && (r.URL.Path == "/v1/pair/commit" || r.URL.Path == "/v1/pair/reveal" || r.URL.Path == "/v1/pair/confirm") {
		if !s.enrollmentOpen() {
			jsonError(w, "not found", http.StatusNotFound)
			return
		}
		s.pairHTTP(w, r)
		return
	}
	// All unsupported paths return 404, including all other /pair/* routes.
	valid := r.Method == http.MethodGet && (r.URL.Path == "/v1/hello" || r.URL.Path == "/v1/snapshot" || r.URL.Path == "/v1/events") || r.Method == http.MethodPost && r.URL.Path == "/v1/ack"
	if !valid {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		jsonError(w, "untrusted viewer", http.StatusForbidden)
		return
	}
	viewer, ok := s.viewer(sha256.Sum256(r.TLS.PeerCertificates[0].Raw))
	if !ok {
		jsonError(w, "untrusted viewer", http.StatusForbidden)
		return
	}
	if !hasPermission(viewer, "read") {
		jsonError(w, "read permission required", http.StatusForbidden)
		return
	}
	switch r.URL.Path {
	case "/v1/hello":
		s.hello(w, r)
	case "/v1/snapshot":
		select {
		case s.snapshots <- struct{}{}:
			defer func() { <-s.snapshots }()
		default:
			w.Header().Set("Retry-After", "1")
			jsonError(w, "snapshot capacity reached", http.StatusServiceUnavailable)
			return
		}
		// Bound the whole build/write, including a viewer that stops reading.
		deadline := time.Now().Add(60 * time.Second)
		_ = http.NewResponseController(w).SetWriteDeadline(deadline)
		ctx, cancel := context.WithDeadline(r.Context(), deadline)
		defer cancel()
		response := &snapshotResponse{ResponseWriter: w}
		if err := s.streamSnapshot(ctx, response); err != nil && !response.started {
			jsonError(w, "snapshot unavailable: "+err.Error(), http.StatusServiceUnavailable)
		}
	case "/v1/events":
		s.events(w, r, viewer)
	case "/v1/ack":
		s.ack(w, r, viewer)
	}
}

// After streaming starts, a failed response stays truncated so the viewer
// rejects it instead of accepting an incomplete snapshot.
type snapshotResponse struct {
	http.ResponseWriter
	started bool
}

func (w *snapshotResponse) Write(p []byte) (int, error) {
	w.started = true
	return w.ResponseWriter.Write(p)
}

func hasPermission(v Viewer, p string) bool {
	for _, x := range v.Permissions {
		if x == p {
			return true
		}
	}
	return false
}
func (s *Server) hello(w http.ResponseWriter, r *http.Request) {
	var value map[string]any
	err := s.st.Tx(r.Context(), func(tx *store.Tx) error {
		m, err := localMachineIdentity(tx)
		if err != nil {
			return err
		}
		epoch, _, seq, err := tx.FeedBounds()
		if err != nil {
			return err
		}
		value = map[string]any{"schema": 1, "observed_at": s.now().UTC(), "driga_version": m.DrigaVersion, "machine_id": tx.MachineID(), "hostname": m.Hostname, "os": m.OS, "arch": m.Arch, "feed_epoch": epoch, "feed_seq": seq, "share_text": s.share(), "capabilities": []string{"snapshot", "events", "ack"}}
		return nil
	})
	if err != nil {
		jsonError(w, "hello unavailable", http.StatusServiceUnavailable)
		return
	}
	_ = json.NewEncoder(w).Encode(value)
}
func (s *Server) ack(w http.ResponseWriter, r *http.Request, v Viewer) {
	if !hasPermission(v, "ack") {
		jsonError(w, "ack permission required", http.StatusForbidden)
		return
	}
	var body struct {
		ItemID string `json:"item_id"`
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&body); err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			jsonError(w, "body too large", 413)
		} else {
			jsonError(w, "invalid ack body", 400)
		}
		return
	}
	endErr := dec.Decode(new(any))
	var large *http.MaxBytesError
	if errors.As(endErr, &large) {
		jsonError(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	if endErr != io.EOF || body.ItemID == "" || len(body.ItemID) > 1024 {
		jsonError(w, "invalid ack body", 400)
		return
	}
	items, err := s.st.Inbox(time.Time{}, true)
	if err != nil {
		jsonError(w, "inbox unavailable", 503)
		return
	}
	found := false
	for _, i := range items {
		if i.ID == body.ItemID {
			found = true
			break
		}
	}
	if !found {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	if err := spool.Append(s.SpoolDir, model.HookEvent{Event: "ack", Adapter: "fleet", Message: body.ItemID, TS: s.now().UTC()}); err != nil {
		jsonError(w, "ack spool unavailable", 503)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_, _ = io.WriteString(w, "{}\n")
}

func parseCursor(raw string) (Cursor, error) {
	epoch, seq, ok := strings.Cut(raw, ":")
	n, err := strconv.ParseInt(seq, 10, 64)
	if !ok || epoch == "" || err != nil || n < 0 {
		return Cursor{}, store.ErrResnapshot
	}
	return Cursor{epoch, n}, nil
}
func (s *Server) resnapshot(w http.ResponseWriter, reason string) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Second))
	s.mu.Lock()
	s.resnapshots++
	s.mu.Unlock()
	b, _ := json.Marshal(feed.ResnapshotRow{Reason: reason})
	_, _ = fmt.Fprintf(w, "event: resnapshot\ndata: %s\n\n", b)
	_ = http.NewResponseController(w).Flush()
}
func writeFrame(w http.ResponseWriter, epoch string, row feed.FeedRow) error {
	b, err := json.Marshal(row)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %s:%d\nevent: %s\ndata: %s\n\n", epoch, row.Seq, row.Kind, b)
	return err
}

func (s *Server) events(w http.ResponseWriter, r *http.Request, v Viewer) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stream := &streamState{device: v.DeviceID, cancel: cancel, presence: r.URL.Query().Get("presence") == "1"}
	s.mu.Lock()
	if s.revoked[v.DeviceID] {
		s.mu.Unlock()
		jsonError(w, "revoked viewer", 403)
		return
	}
	s.streams[stream] = struct{}{}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.streams, stream); s.mu.Unlock() }()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	// Bound blocked network writes, including the final resnapshot frame.
	_ = rc.SetWriteDeadline(time.Now().Add(5 * time.Second))
	cursor, err := parseCursor(r.Header.Get("Last-Event-ID"))
	if err != nil {
		s.resnapshot(w, "missing or invalid cursor")
		return
	}
	wake := s.watch() // Subscribe BEFORE reading to close the snapshot/live race.
	initial, err := s.st.FeedSince(cursor.Epoch, cursor.Seq, streamBuffer+1)
	if errors.Is(err, store.ErrResnapshot) {
		s.resnapshot(w, "epoch or retention mismatch")
		return
	}
	if err != nil {
		return
	}
	if len(initial) > streamBuffer {
		s.resnapshot(w, "slow viewer")
		return
	}
	queue := make(chan feed.FeedRow, streamBuffer)
	for _, row := range initial {
		queue <- row
		cursor.Seq = row.Seq
	}
	slow := make(chan struct{})
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		for {
			select {
			case <-ctx.Done():
				return
			case <-wake:
			}
			wake = s.watch()
			rows, err := s.st.FeedSince(cursor.Epoch, cursor.Seq, streamBuffer+1)
			if err != nil {
				close(slow)
				return
			}
			for _, row := range rows {
				select {
				case queue <- row:
					cursor.Seq = row.Seq
				default:
					close(slow)
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-producerDone }()
	if stream.presence {
		_ = s.RefreshPresence()
	}
	_, _ = io.WriteString(w, "retry: 2000\n\n")
	if rc.Flush() != nil {
		return
	}
	ping := s.newTicker(15 * time.Second)
	defer ping.Stop()
	epoch := cursor.Epoch
	for {
		select {
		case <-ctx.Done():
			return
		case <-slow:
			s.resnapshot(w, "slow viewer or unavailable cursor")
			return
		default:
		}
		select {
		case <-ctx.Done():
			return
		case <-slow:
			s.resnapshot(w, "slow viewer or unavailable cursor")
			return
		case row := <-queue:
			_ = rc.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if writeFrame(w, epoch, row) != nil || rc.Flush() != nil {
				return
			}
		case <-ping.Chan():
			_ = rc.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil || rc.Flush() != nil {
				return
			}
		}
	}
}

// RefreshPresence renews the shared heartbeat at most once every four seconds.
// The collector calls it on its existing wake, so idle streams need no timers
// beyond their 15-second pings.
func (s *Server) RefreshPresence() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for stream := range s.streams {
		if !stream.presence {
			continue
		}
		now := s.now()
		if !s.lastHeartbeat.IsZero() && now.Before(s.lastHeartbeat.Add(4*time.Second)) {
			return nil
		}
		if err := s.touchHeartbeat(); err != nil {
			return err
		}
		s.lastHeartbeat = now
		return nil
	}
	return nil
}

func (s *Server) touchHeartbeat() error {
	path := filepath.Join(s.dataDir, "viewer.heartbeat")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	err = f.Chmod(0600)
	closeErr := f.Close()
	now := s.now()
	return errors.Join(err, closeErr, os.Chtimes(path, now, now))
}

type admission struct {
	mu       sync.Mutex
	now      func() time.Time
	accepted []time.Time
	active   int
}

func newAdmission(now func() time.Time) *admission { return &admission{now: now} }
func (a *admission) acquire() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	for len(a.accepted) > 0 && !now.Before(a.accepted[0].Add(time.Second)) {
		a.accepted = a.accepted[1:]
	}
	if len(a.accepted) >= 10 || a.active >= 16 {
		return false
	}
	a.accepted = append(a.accepted, now)
	a.active++
	return true
}
func (a *admission) release() { a.mu.Lock(); a.active--; a.mu.Unlock() }

type limitedListener struct {
	net.Listener
	admission *admission
}

func (l *limitedListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if !l.admission.acquire() {
			_ = c.Close()
			continue
		}
		return &limitedConn{Conn: c, release: l.admission.release}, nil
	}
}

type limitedConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *limitedConn) Close() error { err := c.Conn.Close(); c.once.Do(c.release); return err }

func jsonError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// localMachineIdentity is available from meta before the first collection pass.
func localMachineIdentity(r interface {
	Machine() (model.Machine, error)
	MachineID() string
}) (model.Machine, error) {
	m, err := r.Machine()
	if !errors.Is(err, store.ErrNotFound) {
		return m, err
	}
	host, err := os.Hostname()
	if err != nil {
		return m, err
	}
	return model.Machine{MachineID: r.MachineID(), Hostname: host, OS: runtime.GOOS, Arch: runtime.GOARCH}, nil
}
