package fleet

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
)

func serverFixture(t *testing.T) (*Server, *store.Store, Identity) {
	return serverFixtureBeforePass(t, false)
}

func serverFixtureBeforePass(t *testing.T, beforePass bool) (*Server, *store.Store, Identity) {
	t.Helper()
	// Unix socket paths have a small OS limit; Go's per-test directory embeds
	// the test name and can exceed it on macOS.
	dir, err := os.MkdirTemp("/tmp", "driga-fleet-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("HOME", dir)
	t.Setenv("DRIGA_DATA_DIR", dir)
	t.Setenv("DRIGA_SPOOL_DIR", filepath.Join(dir, "spool"))
	t.Setenv("DRIGA_CONFIG", filepath.Join(dir, "config"))
	st, err := store.Open(filepath.Join(dir, "driga.db"), store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	id, err := LoadOrCreateIdentity(filepath.Join(dir, "fleet"))
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := LoadOrCreateIdentity(filepath.Join(t.TempDir(), "fleet"))
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveViewers(filepath.Join(dir, "fleet"), []Viewer{{DeviceID: viewer.DeviceID, Permissions: []string{"read", "ack"}}}); err != nil {
		t.Fatal(err)
	}
	if !beforePass {
		if err := st.Tx(context.Background(), func(tx *store.Tx) error {
			return tx.UpsertMachine(model.Machine{Hostname: "test", OS: "linux", Arch: "amd64", DrigaVersion: "test"})
		}); err != nil {
			t.Fatal(err)
		}
	}
	return NewServer(st, id, dir, config.Default()), st, viewer
}
func request(s *Server, id Identity, method, path string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{id.Cert.Leaf}}
	return r
}
func appendRows(t *testing.T, st *store.Store, n int, at time.Time) {
	t.Helper()
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		for i := 0; i < n; i++ {
			if _, err := tx.AppendFeed("event", fmt.Sprint(i), []byte(`{"kind":"test"}`), at); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestHelloShape(t *testing.T) {
	s, st, id := serverFixture(t)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, request(s, id, "GET", "/v1/hello"))
	var v map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"schema", "driga_version", "machine_id", "hostname", "os", "arch", "feed_epoch", "feed_seq", "share_text", "capabilities"} {
		if _, ok := v[k]; !ok {
			t.Errorf("missing %s: %s", k, w.Body)
		}
	}
	if v["machine_id"] != st.MachineID() {
		t.Fatal(v)
	}
}
func TestSnapshotCursorMatchesTx(t *testing.T) {
	s, st, id := serverFixture(t)
	appendRows(t, st, 1, time.Now())
	// Pin a WAL read before the concurrent commit, then use the exact snapshot
	// builder used by the endpoint on that transaction.
	db, err := sql.Open("sqlite", "file:"+filepath.Join(s.dataDir, "driga.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var epoch string
	if err := tx.QueryRow("SELECT value FROM meta WHERE key='feed_epoch'").Scan(&epoch); err != nil {
		t.Fatal(err)
	}
	if err := st.Tx(context.Background(), func(w *store.Tx) error {
		if _, err := w.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "after"}}); err != nil {
			return err
		}
		_, err := w.AppendFeed("session", "after", []byte(`{"external_id":"after"}`), time.Now())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	pinned, err := s.snapshotTx(context.Background(), tx)
	if err != nil || pinned.Data.Cursor.Seq != 1 || len(pinned.Data.Sessions) != 0 {
		t.Fatalf("pinned snapshot: %+v %v", pinned, err)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, request(s, id, "GET", "/v1/snapshot"))
	var snap Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Data.Cursor.Seq != 2 || len(snap.Data.Sessions) != 1 {
		t.Fatal(w.Body.String())
	}
	appendRows(t, st, 1, time.Now())
	rows, err := st.FeedSince(snap.Data.Cursor.Epoch, snap.Data.Cursor.Seq, 0)
	if err != nil || len(rows) != 1 || rows[0].Seq != 3 {
		t.Fatalf("%+v %v", rows, err)
	}
}
func TestSnapshotIncludesReferencedSessions(t *testing.T) {
	s, st, id := serverFixture(t)
	old := time.Now().Add(-3 * 24 * time.Hour)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		parent, e := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "parent"}, EndedAt: &old})
		if e != nil {
			return e
		}
		child, e := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "old"}, ParentID: &parent, EndedAt: &old})
		if e != nil {
			return e
		}
		return tx.UpsertInboxItem(model.InboxItem{ID: "item", SessionID: child, TS: time.Now()})
	}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, request(s, id, "GET", "/v1/snapshot"))
	var v Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Data.Sessions) != 2 {
		t.Fatal(w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"session_id"`) || strings.Contains(w.Body.String(), `"parent_id"`) {
		t.Fatal(w.Body.String())
	}
}

// streamWriter makes each flush observable without a network or real-time sleeps.
type streamWriter struct {
	header http.Header
	frames chan string
	mu     sync.Mutex
	buf    strings.Builder
}

func newStreamWriter() *streamWriter {
	return &streamWriter{header: make(http.Header), frames: make(chan string, 2048)}
}
func (w *streamWriter) Header() http.Header { return w.header }
func (w *streamWriter) WriteHeader(int)     {}
func (w *streamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}
func (w *streamWriter) Flush() {
	w.mu.Lock()
	v := w.buf.String()
	w.buf.Reset()
	w.mu.Unlock()
	w.frames <- v
}
func nextFrame(t *testing.T, w *streamWriter) string {
	t.Helper()
	select {
	case v := <-w.frames:
		return v
	case <-time.After(time.Second):
		t.Fatal("no frame")
		return ""
	}
}
func stream(t *testing.T, s *Server, id Identity, cursor, path string) (*streamWriter, context.CancelFunc, <-chan struct{}) {
	t.Helper()
	r := request(s, id, "GET", path)
	r.Header.Set("Last-Event-ID", cursor)
	ctx, cancel := context.WithCancel(r.Context())
	w := newStreamWriter()
	done := make(chan struct{})
	go func() { defer close(done); s.ServeHTTP(w, r.WithContext(ctx)) }()
	t.Cleanup(func() { cancel(); <-done })
	return w, cancel, done
}
func TestEventsResumeExact(t *testing.T) {
	s, st, id := serverFixture(t)
	appendRows(t, st, 3, time.Now())
	epoch, _, _, _ := st.FeedBounds()
	w, _, _ := stream(t, s, id, epoch+":1", "/v1/events")
	var frames string
	for !strings.Contains(frames, "id: "+epoch+":3") {
		frames += nextFrame(t, w)
	}
	if strings.Contains(frames, "id: "+epoch+":1\n") || !strings.Contains(frames, "id: "+epoch+":2\n") {
		t.Fatal(frames)
	}
	appendRows(t, st, 1, time.Now())
	s.Notify()
	if v := nextFrame(t, w); !strings.Contains(v, epoch+":4") {
		t.Fatal(v)
	}
}
func TestResumeAfterPruneResnapshots(t *testing.T) {
	s, st, id := serverFixture(t)
	appendRows(t, st, 3, time.Now().Add(-31*24*time.Hour))
	if err := st.Tx(context.Background(), func(tx *store.Tx) error { return tx.Prune(time.Now()) }); err != nil {
		t.Fatal(err)
	}
	epoch, _, _, _ := st.FeedBounds()
	w, _, done := stream(t, s, id, epoch+":1", "/v1/events")
	if v := nextFrame(t, w); !strings.Contains(v, "event: resnapshot") {
		t.Fatal(v)
	}
	<-done
}
func TestEpochMismatchResnapshots(t *testing.T) {
	s, _, id := serverFixture(t)
	w, _, done := stream(t, s, id, "wrong:0", "/v1/events")
	if v := nextFrame(t, w); !strings.Contains(v, "event: resnapshot") {
		t.Fatal(v)
	}
	<-done
}
func TestSlowViewerGetsResnapshot(t *testing.T) {
	s, st, id := serverFixture(t)
	appendRows(t, st, 1001, time.Now())
	epoch, _, _, _ := st.FeedBounds()
	w, _, done := stream(t, s, id, epoch+":0", "/v1/events")
	if v := nextFrame(t, w); !strings.Contains(v, "event: resnapshot") {
		t.Fatal(v)
	}
	<-done
}

type fakeTicker struct{ c chan time.Time }

func (f *fakeTicker) Chan() <-chan time.Time { return f.c }
func (f *fakeTicker) Stop()                  {}
func fakeTicks(s *Server) (chan time.Duration, chan *fakeTicker) {
	ds := make(chan time.Duration, 4)
	ts := make(chan *fakeTicker, 4)
	s.newTicker = func(d time.Duration) ticker { f := &fakeTicker{make(chan time.Time, 10)}; ds <- d; ts <- f; return f }
	return ds, ts
}
func TestPingEvery15s(t *testing.T) {
	s, st, id := serverFixture(t)
	ds, ts := fakeTicks(s)
	epoch, _, _, _ := st.FeedBounds()
	w, _, _ := stream(t, s, id, epoch+":0", "/v1/events")
	nextFrame(t, w)
	if d := <-ds; d != 15*time.Second {
		t.Fatal(d)
	}
	f := <-ts
	for i := 0; i < 3; i++ {
		f.c <- time.Now()
		if v := nextFrame(t, w); v != ": ping\n\n" {
			t.Fatal(v)
		}
	}
}
func TestRevokeClosesStreamWithin1s(t *testing.T) {
	s, st, id := serverFixture(t)
	epoch, _, _, _ := st.FeedBounds()
	w, _, done := stream(t, s, id, epoch+":0", "/v1/events")
	nextFrame(t, w)
	s.Revoke(id.DeviceID)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream survived revocation")
	}
	w2 := httptest.NewRecorder()
	s.ServeHTTP(w2, request(s, id, "GET", "/v1/hello"))
	if w2.Code != http.StatusForbidden {
		t.Fatal(w2.Code)
	}
}
func TestUnknownCertRejectedOutsideEnrollment(t *testing.T) {
	s, _, _ := serverFixture(t)
	unknown, err := LoadOrCreateIdentity(filepath.Join(t.TempDir(), "fleet"))
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()
	defer func() { cancel(); <-done }()
	tr := &http.Transport{TLSClientConfig: PinnedTLS(unknown, func([32]byte) bool { return true })}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: time.Second}
	if r, e := client.Get("https://" + ln.Addr().String() + "/v1/hello"); e == nil {
		r.Body.Close()
		t.Fatal("unknown certificate accepted")
	}
}
func TestAcceptLimits(t *testing.T) {
	now := time.Now()
	a := newAdmission(func() time.Time { return now })
	for i := 0; i < 10; i++ {
		if !a.acquire() {
			t.Fatal(i)
		}
	}
	if a.acquire() {
		t.Fatal("rate cap exceeded")
	}
	now = now.Add(time.Second)
	for i := 0; i < 6; i++ {
		if !a.acquire() {
			t.Fatal(i)
		}
	}
	now = now.Add(time.Second)
	if a.acquire() {
		t.Fatal("connection cap exceeded")
	}
	a.release()
	if !a.acquire() {
		t.Fatal("slot not released")
	}
}
func TestPresenceTouchesHeartbeat(t *testing.T) {
	s, st, id := serverFixture(t)
	ds, ts := fakeTicks(s)
	epoch, _, _, _ := st.FeedBounds()
	w, cancel, done := stream(t, s, id, epoch+":0", "/v1/events?presence=1")
	nextFrame(t, w)
	if d := <-ds; d != 15*time.Second {
		t.Fatalf("stream timer = %v; want only the ping timer", d)
	}
	<-ts
	if _, err := os.Stat(filepath.Join(s.dataDir, "viewer.heartbeat")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.dataDir, "viewer.heartbeat")
	old := time.Unix(1, 0)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	// The daemon's existing wake drives the four-second renewal; the
	// stream itself only owns its ping timer.
	s.mu.Lock()
	s.lastHeartbeat = time.Now().Add(-4 * time.Second)
	s.mu.Unlock()
	if err := s.RefreshPresence(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().After(old) {
		t.Fatal("collector wake did not touch heartbeat")
	}
	cancel()
	<-done
}
func TestPairRoutesOnlyDuringEnrollment(t *testing.T) {
	s, _, id := serverFixture(t)
	for _, open := range []bool{false, true} {
		if open {
			if err := s.OpenEnrollment(time.Minute); err != nil {
				t.Fatal(err)
			}
		}
		for _, path := range []string{"commit", "reveal", "confirm"} {
			w := httptest.NewRecorder()
			s.ServeHTTP(w, request(s, id, "POST", "/v1/pair/"+path))
			want := 404
			if open {
				want = 400
			}
			if w.Code != want {
				t.Fatalf("%s: %d", path, w.Code)
			}
		}
	}
}

func TestAckSpoolsOnlyAuthorizedKnownItem(t *testing.T) {
	s, st, id := serverFixture(t)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error { return tx.UpsertInboxItem(model.InboxItem{ID: "item", TS: time.Now()}) }); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		body       string
		permission bool
		want       int
	}{{`{"item_id":"missing"}`, true, 404}, {`{"item_id":"item"}`, false, 403}, {`{}`, true, 400}, {`{"item_id":"item"} {}`, true, 400}, {`{"item_id":"item"}`, true, 202}} {
		permissions := []string{"read"}
		if tc.permission {
			permissions = append(permissions, "ack")
		}
		if err := SaveViewers(filepath.Join(s.dataDir, "fleet"), []Viewer{{DeviceID: id.DeviceID, Permissions: permissions}}); err != nil {
			t.Fatal(err)
		}
		r := request(s, id, "POST", "/v1/ack")
		r.Body = io.NopCloser(strings.NewReader(tc.body))
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%s: %d %s", tc.body, w.Code, w.Body)
		}
	}
	events, _, err := spool.Drain(s.SpoolDir, nil)
	if err != nil || len(events) != 1 || events[0].Event != "ack" || events[0].Adapter != "fleet" || events[0].Message != "item" {
		t.Fatalf("spool %+v %v", events, err)
	}
	items, err := st.Inbox(time.Time{}, true)
	if err != nil || items[0].AckedAt != nil {
		t.Fatalf("HTTP wrote DB: %+v %v", items, err)
	}
}

func TestSnapshotAllReferencesAndPrivacy(t *testing.T) {
	s, st, _ := serverFixture(t)
	s.cfg.CaptureText = "snippets"
	s.cfg.Fleet.ShareText = "snippets"
	now := time.Now().UTC()
	s.now = func() time.Time { return now }
	old := now.Add(-3 * 24 * time.Hour)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		repo, err := tx.UpsertRepo(model.Repo{Path: "/r", RemoteURL: "https://u:p@host/r"})
		if err != nil {
			return err
		}
		work, err := tx.UpsertWorktree(model.Worktree{RepoID: repo, Path: "/r/w", LastCommitSubject: "commit"})
		if err != nil {
			return err
		}
		ids := map[string]int64{}
		for _, name := range []string{"files", "events", "handoff", "proc", "excluded"} {
			id, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: name, Title: "title", LastPrompt: "private prompt"}, EndedAt: &old, RepoID: &repo, WorktreeID: &work})
			if err != nil {
				return err
			}
			ids[name] = id
		}
		if err := tx.ReplaceSessionFiles(ids["files"], []model.FileTouch{{Path: "/r/a", TS: now}}); err != nil {
			return err
		}
		if err := tx.InsertEvent(model.Event{Adapter: "claude", ExternalID: "events", TS: now, Kind: "Stop", Summary: "private event"}); err != nil {
			return err
		}
		if err := tx.InsertHandoff(ids["handoff"], now, "private handoff"); err != nil {
			return err
		}
		if err := tx.ReplaceProcs([]model.Proc{{PID: 1, Agent: "claude", SessionID: ids["proc"], Cmdline: "claude ghp_abcdefghijklmnopqrstuvwxyz1234567890"}, {PID: 2, Cmdline: "unrelated process"}}); err != nil {
			return err
		}
		if err := tx.ReplaceOrphans([]model.Orphan{{ID: "orphan", PIDs: []int32{1}, Evidence: []string{"reparented"}, Verified: true}}); err != nil {
			return err
		}
		if err := tx.InsertSample(model.MachineSample{TS: now, Cores: 4}); err != nil {
			return err
		}
		if err := tx.SetSourceOK("procs", now); err != nil {
			return err
		}
		if err := tx.InsertGap(model.Gap{From: old, To: now}); err != nil {
			return err
		}
		remote := tx.ForMachine("remote")
		_, err = remote.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "remote"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	snap, err := s.snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Data.Sessions) != 4 || len(snap.Data.Handoffs) != 1 || len(snap.Data.SessionFiles) != 1 || len(snap.Data.Procs) != 1 || len(snap.Data.Orphans) != 1 {
		t.Fatalf("snapshot %+v", snap.Data)
	}
	b, _ := json.Marshal(snap)
	text := string(b)
	for _, bad := range []string{`"repo_id"`, `"worktree_id"`, `"session_id"`, `"parent_id"`, `"id":1`, `https://u:p`, "unrelated process", "ghp_", `"external_id":"remote"`, `"external_id":"excluded"`} {
		if strings.Contains(text, bad) {
			t.Errorf("snapshot contains %s", bad)
		}
	}
	s.cfg.Fleet.ShareText = "none"
	snap, err = s.snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(snap)
	if len(snap.Data.Handoffs) != 0 || strings.Contains(string(b), "private") || strings.Contains(string(b), `"title":"title"`) || strings.Contains(string(b), `"last_commit_subject":"commit"`) {
		t.Fatal(string(b))
	}
}

func TestControlJSONLinesAndModes(t *testing.T) {
	s, _, id := serverFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := s.listenControl(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	path := filepath.Join(s.dataDir, "fleet", "fleet.sock")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("socket: %v %v", info, err)
	}
	for _, req := range []ControlRequest{{Op: "status"}, {Op: "enroll", Window: "1m"}, {Op: "revoke", DeviceID: id.DeviceID}} {
		resp, err := Control(ctx, s.dataDir, req)
		if err != nil || !resp.OK {
			t.Fatalf("%+v: %+v %v", req, resp, err)
		}
	}
	info, err = os.Stat(filepath.Join(s.dataDir, "fleet", "enroll.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("enroll: %v %v", info, err)
	}
	if !s.Status().EnrollmentOpen {
		t.Fatal("enrollment not open")
	}
	if _, err := Control(ctx, s.dataDir, ControlRequest{Op: "invalid"}); err == nil {
		t.Fatal("unknown control operation accepted")
	}
}

func TestMTLSRoutesEnrollmentAndLiveRevoke(t *testing.T) {
	s, st, id := serverFixture(t)
	ro, err := store.Open(filepath.Join(s.dataDir, "driga.db"), store.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	s.st = ro
	unknown, err := LoadOrCreateIdentity(filepath.Join(t.TempDir(), "fleet"))
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	clientFor := func(identity Identity) *http.Client {
		tr := &http.Transport{TLSClientConfig: PinnedTLS(identity, func(fp [32]byte) bool { pin, _ := ParseDeviceID(s.id.DeviceID); return EqualFingerprint(pin, fp) })}
		t.Cleanup(tr.CloseIdleConnections)
		return &http.Client{Transport: tr, Timeout: 3 * time.Second}
	}
	knownClient := clientFor(id)
	url := "https://" + ln.Addr().String()
	resp, err := knownClient.Get(url + "/v1/snapshot")
	if err != nil {
		t.Fatal(err)
	}
	var snap Snapshot
	err = json.NewDecoder(resp.Body).Decode(&snap)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("snapshot %d %v", resp.StatusCode, err)
	}
	if _, err := Control(ctx, s.dataDir, ControlRequest{Op: "enroll", Window: "1m"}); err != nil {
		t.Fatal(err)
	}
	unpaired := clientFor(unknown)
	for _, tc := range []struct {
		method, path string
		want         int
	}{{"POST", "/v1/pair/commit", 400}, {"GET", "/v1/hello", 403}, {"GET", "/anything", 404}, {"POST", "/v1/pair/other", 404}} {
		req, _ := http.NewRequest(tc.method, url+tc.path, nil)
		resp, err := unpaired.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Fatalf("%s: %d", tc.path, resp.StatusCode)
		}
	}
	req, _ := http.NewRequest("GET", url+"/v1/events", nil)
	req.Header.Set("Last-Event-ID", snap.Data.Cursor.Epoch+":"+fmt.Sprint(snap.Data.Cursor.Seq))
	resp, err = knownClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	line, err := reader.ReadString('\n')
	if err != nil || line != "retry: 2000\n" {
		t.Fatalf("%q %v", line, err)
	}
	appendRows(t, st, 1, time.Now())
	s.Notify()
	found := false
	for !found {
		line, err = reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		found = strings.HasPrefix(line, "id: ")
	}
	if _, err := Control(ctx, s.dataDir, ControlRequest{Op: "revoke", DeviceID: id.DeviceID}); err != nil {
		t.Fatal(err)
	}
	eof := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, reader); close(eof) }()
	select {
	case <-eof:
	case <-time.After(time.Second):
		t.Fatal("network stream survived revoke")
	}
}

func TestAckBodyLimit(t *testing.T) {
	s, _, id := serverFixture(t)
	r := request(s, id, "POST", "/v1/ack")
	r.Body = io.NopCloser(strings.NewReader(`{"item_id":"x"}` + strings.Repeat(" ", 8<<20)))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 413 || w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestServeRefusesStaleIdentityAfterDBRecreate(t *testing.T) {
	s, st, _ := serverFixture(t)
	oldID := st.MachineID()
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(s.dataDir, "driga.db")); err != nil {
		t.Fatal(err)
	}
	replacement, err := store.Open(filepath.Join(s.dataDir, "driga.db"), store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	if replacement.MachineID() == oldID {
		t.Fatal("database identity did not change")
	}
	s.st = replacement
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err = s.Serve(ctx, ln)
	want := "identity belongs to machine " + oldID + "; run `driga fleet rekey` (pairings must be redone)"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("serve stale identity: %v; want %q", err, want)
	}
	current, err := LoadOrCreateIdentity(filepath.Join(s.dataDir, "fleet"))
	if err != nil || current.DeviceID != s.id.DeviceID {
		t.Fatalf("identity silently replaced: %v", err)
	}
}

// snapshotPipeWriter makes a non-reading client block writes without filling
// kernel buffers. It scales a production deadline down for a fast regression.
type snapshotPipeWriter struct {
	conn     net.Conn
	header   http.Header
	entered  chan struct{}
	deadline time.Time
	once     sync.Once
}

func (w *snapshotPipeWriter) Header() http.Header { return w.header }
func (w *snapshotPipeWriter) WriteHeader(int)     {}
func (w *snapshotPipeWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	return w.conn.Write(p)
}
func (w *snapshotPipeWriter) SetWriteDeadline(d time.Time) error {
	w.deadline = d
	return w.conn.SetWriteDeadline(time.Now().Add(30 * time.Millisecond))
}
func newSnapshotPipeWriter() (*snapshotPipeWriter, net.Conn) {
	a, b := net.Pipe()
	return &snapshotPipeWriter{conn: a, header: make(http.Header), entered: make(chan struct{})}, b
}
func TestSlowSnapshotReaderTimesOut(t *testing.T) {
	s, _, id := serverFixture(t)
	w, reader := newSnapshotPipeWriter()
	defer w.conn.Close()
	defer reader.Close()
	done := make(chan struct{})
	go func() { defer close(done); s.ServeHTTP(w, request(s, id, "GET", "/v1/snapshot")) }()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		reader.Close()
		<-done
		t.Fatal("snapshot write blocked without a bounded deadline")
	}
	if w.deadline.IsZero() || time.Until(w.deadline) > 61*time.Second {
		t.Fatalf("unbounded snapshot deadline: %s", w.deadline)
	}
}
func TestSnapshotConcurrencyLimit(t *testing.T) {
	s, _, id := serverFixture(t)
	var done []chan struct{}
	t.Cleanup(func() {
		for _, ch := range done {
			select {
			case <-ch:
			case <-time.After(time.Second):
				t.Error("snapshot handler did not exit")
			}
		}
	})
	for range 2 {
		w, reader := newSnapshotPipeWriter()
		// Keep these writes blocked while the third request is admitted.
		w.conn = &snapshotNoDeadlineConn{Conn: w.conn}
		t.Cleanup(func() { reader.Close(); w.conn.Close() })
		ch := make(chan struct{})
		done = append(done, ch)
		go func() { defer close(ch); s.ServeHTTP(w, request(s, id, "GET", "/v1/snapshot")) }()
		select {
		case <-w.entered:
		case <-time.After(time.Second):
			t.Fatal("snapshot did not start")
		}
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, request(s, id, "GET", "/v1/snapshot"))
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") == "" {
		t.Errorf("extra snapshot: status=%d retry=%q", w.Code, w.Header().Get("Retry-After"))
	}

}

type snapshotNoDeadlineConn struct{ net.Conn }

func (*snapshotNoDeadlineConn) SetWriteDeadline(time.Time) error { return nil }

// Materialisation is only for assertions; production writes directly to HTTP.
func (s *Server) snapshot(ctx context.Context) (*Snapshot, error) {
	var b bytes.Buffer
	if err := s.streamSnapshot(ctx, &b); err != nil {
		return nil, err
	}
	var snap Snapshot
	err := json.Unmarshal(b.Bytes(), &snap)
	return &snap, err
}
func (s *Server) snapshotTx(ctx context.Context, tx *sql.Tx) (*Snapshot, error) {
	var b bytes.Buffer
	if err := s.streamSnapshotTx(ctx, tx, &b); err != nil {
		return nil, err
	}
	var snap Snapshot
	err := json.Unmarshal(b.Bytes(), &snap)
	return &snap, err
}
