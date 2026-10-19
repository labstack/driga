package fleet

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func pairServer(t *testing.T) (*Server, string, string) {
	t.Helper()
	return startPairServer(t, false)
}

func startPairServer(t *testing.T, beforePass bool) (*Server, string, string) {
	t.Helper()
	s, _, _ := serverFixtureBeforePass(t, beforePass)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	sock := s.dataDir // Enroll addresses the collector by data dir
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := Control(ctx, s.dataDir, ControlRequest{Op: "status"}); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("control not ready")
		}
		time.Sleep(time.Millisecond)
	}
	return s, ln.Addr().String(), sock
}

func pairIdentity(t *testing.T) (string, Identity) {
	t.Helper()
	dir := t.TempDir()
	id, err := LoadOrCreateIdentity(filepath.Join(dir, "fleet"))
	if err != nil {
		t.Fatal(err)
	}
	return dir, id
}

func TestPairCodeVector(t *testing.T) {
	var nv, np, fv, fp [32]byte
	ekm := make([]byte, 32)
	for i := range nv {
		nv[i] = byte(i)
		np[i] = byte(i + 32)
		fv[i] = byte(i + 64)
		fp[i] = byte(i + 96)
		ekm[i] = byte(i + 128)
	}
	if got := PairCode(nv, np, fv, fp, ekm); got != "833 475" {
		t.Fatalf("vector %s", got)
	}
	var b bytes.Buffer
	binary.Write(&b, binary.BigEndian, uint32(0xffffffff))
	binary.Write(&b, binary.BigEndian, uint32(4294000000))
	binary.Write(&b, binary.BigEndian, uint32(999999))
	code, err := samplePairCode(&b)
	if err != nil || code != "999 999" {
		t.Fatalf("rejection sampling %s %v", code, err)
	}
}

func TestPairHappyPath(t *testing.T) {
	s, addr, sock := pairServer(t)
	dir, _ := pairIdentity(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	codes := make(chan string, 2)
	enrolled := make(chan error, 1)
	go func() {
		_, e := Enroll(ctx, sock, EnrollOpts{Ack: true}, func(code string) bool { codes <- code; return true })
		enrolled <- e
	}()
	waitEnrollment(t, s)
	peer, err := Add(ctx, addr, AddOpts{DataDir: dir, Name: "vm"}, func(code string) bool { codes <- code; return true })
	if err != nil {
		t.Fatal(err)
	}
	if err := <-enrolled; err != nil {
		t.Fatal(err)
	}
	if a, b := <-codes, <-codes; a != b {
		t.Fatalf("codes %s != %s", a, b)
	}
	peers, _ := LoadPeers(filepath.Join(dir, "fleet"))
	viewers, _ := LoadViewers(filepath.Join(s.dataDir, "fleet"))
	if !peer.Ack || len(peers) != 1 || peers[0] != peer || peer.MachineID != s.st.MachineID() || len(viewers) != 2 || !hasPermission(viewers[1], "ack") || s.Status().EnrollmentOpen {
		t.Fatalf("persist %+v %+v", peers, viewers)
	}
	if _, err := os.Stat(filepath.Join(s.dataDir, "fleet", "enroll.json")); !os.IsNotExist(err) {
		t.Fatalf("window file: %v", err)
	}
}

func waitEnrollment(t *testing.T, s *Server) {
	t.Helper()
	limit := time.After(3 * time.Second)
	for !s.Status().EnrollmentOpen {
		select {
		case <-limit:
			t.Fatal("no window")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestPairRejectsWrongCommitment(t *testing.T) {
	s, addr, _ := pairServer(t)
	_, id := pairIdentity(t)
	if err := s.OpenEnrollment(time.Minute); err != nil {
		t.Fatal(err)
	}
	c, err := dialPair(context.Background(), addr, id, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var response pairMessage
	if err := c.post(context.Background(), "commit", pairMessage{Commit: strings.Repeat("a", 64)}, &response); err != nil {
		t.Fatal(err)
	}
	if err := c.post(context.Background(), "reveal", pairMessage{Nonce: strings.Repeat("0", 64)}, &response); err == nil {
		t.Fatal("wrong commitment accepted")
	}
	viewers, _ := LoadViewers(filepath.Join(s.dataDir, "fleet"))
	if len(viewers) != 1 {
		t.Fatal(viewers)
	}
}

func TestPairReplayNonceFails(t *testing.T) {
	s, addr, _ := pairServer(t)
	_, id := pairIdentity(t)
	s.OpenEnrollment(time.Minute)
	var nv [32]byte
	nv[0] = 5
	commit := sha256.Sum256(nv[:])
	for i := 0; i < 2; i++ {
		c, err := dialPair(context.Background(), addr, id, "")
		if err != nil {
			t.Fatal(err)
		}
		var response pairMessage
		err = c.post(context.Background(), "commit", pairMessage{Commit: fmt.Sprintf("%x", commit)}, &response)
		c.Close()
		if (i == 0) != (err == nil) {
			t.Fatalf("replay %d: %v", i, err)
		}
	}
}

func TestPairAttemptCapAndWindowExpiry(t *testing.T) {
	s, addr, _ := pairServer(t)
	_, id := pairIdentity(t)
	s.OpenEnrollment(time.Minute)
	for i := 0; i < 3; i++ {
		c, err := dialPair(context.Background(), addr, id, "")
		if err != nil {
			t.Fatal(err)
		}
		var response pairMessage
		c.post(context.Background(), "commit", pairMessage{}, &response)
		c.Close()
	}
	if s.Status().EnrollmentOpen {
		t.Fatal("attempt cap not enforced")
	}
	if _, err := os.Stat(filepath.Join(s.dataDir, "fleet", "enroll.json")); !os.IsNotExist(err) {
		t.Fatal("exhausted window persisted")
	}
	s.OpenEnrollment(time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	if s.Status().EnrollmentOpen {
		t.Fatal("expired window open")
	}
}

func TestPairEitherNoAborts(t *testing.T) {
	for _, side := range []string{"viewer", "peer"} {
		t.Run(side, func(t *testing.T) {
			s, addr, sock := pairServer(t)
			dir, _ := pairIdentity(t)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := Enroll(ctx, sock, EnrollOpts{}, func(string) bool { return side != "peer" })
				done <- err
			}()
			waitEnrollment(t, s)
			if _, err := Add(ctx, addr, AddOpts{DataDir: dir}, func(string) bool { return side != "viewer" }); err == nil {
				t.Fatal("decline accepted")
			}
			cancel()
			<-done
			peers, _ := LoadPeers(filepath.Join(dir, "fleet"))
			vs, _ := LoadViewers(filepath.Join(s.dataDir, "fleet"))
			if len(peers) != 0 || len(vs) != 1 {
				t.Fatal("decline saved trust")
			}
		})
	}
}

func TestHeadlessAllowID(t *testing.T) {
	s, addr, sock := pairServer(t)
	dir, id := pairIdentity(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	panicPrompt := func(string) bool { t.Error("headless prompt"); return false }
	if _, err := Enroll(ctx, sock, EnrollOpts{AllowID: id.DeviceID}, panicPrompt); err != nil {
		t.Fatal(err)
	}
	_, wrong := pairIdentity(t)
	if _, err := Add(ctx, addr, AddOpts{DataDir: dir, ID: wrong.DeviceID}, panicPrompt); err == nil {
		t.Fatal("wrong server id accepted")
	}
	badDir, _ := pairIdentity(t)
	if _, err := Add(ctx, addr, AddOpts{DataDir: badDir, ID: s.id.DeviceID}, panicPrompt); err == nil {
		t.Fatal("wrong viewer id accepted")
	}
	if _, err := Add(ctx, addr, AddOpts{DataDir: dir, ID: s.id.DeviceID}, panicPrompt); err != nil {
		t.Fatal(err)
	}
}

func TestIdentityChangeMarksUntrusted(t *testing.T) {
	_, addr, sock := pairServer(t)
	dir, id := pairIdentity(t)
	_, old := pairIdentity(t)
	SavePeers(filepath.Join(dir, "fleet"), []Peer{{Name: "vm", Addr: addr, DeviceID: old.DeviceID, MachineID: "remote"}})
	Enroll(context.Background(), sock, EnrollOpts{AllowID: id.DeviceID}, nil)
	_, err := Add(context.Background(), addr, AddOpts{DataDir: dir}, nil)
	if err == nil || !strings.Contains(err.Error(), "untrusted") || !strings.Contains(err.Error(), "remove") {
		t.Fatal(err)
	}
	peers, _ := LoadPeers(filepath.Join(dir, "fleet"))
	if peers[0].DeviceID != old.DeviceID || peers[0].State != "untrusted" {
		t.Fatal(peers)
	}
}

func TestCloneSameMachineIDRefused(t *testing.T) {
	for _, known := range []string{"peers", "machines"} {
		t.Run(known, func(t *testing.T) {
			s, addr, sock := pairServer(t)
			dir, id := pairIdentity(t)
			_, other := pairIdentity(t)
			if known == "peers" {
				SavePeers(filepath.Join(dir, "fleet"), []Peer{{Name: "vm-1", Addr: "127.0.0.1:1", DeviceID: other.DeviceID, MachineID: s.st.MachineID()}})
			} else {
				st, err := store.Open(filepath.Join(dir, "driga.db"), store.ReadWrite)
				if err != nil {
					t.Fatal(err)
				}
				err = st.Tx(context.Background(), func(tx *store.Tx) error {
					return tx.ForMachine(s.st.MachineID()).UpsertMachine(model.Machine{Name: "vm-1", DeviceID: other.DeviceID})
				})
				st.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			Enroll(context.Background(), sock, EnrollOpts{AllowID: id.DeviceID}, nil)
			_, err := Add(context.Background(), addr, AddOpts{DataDir: dir, ID: s.id.DeviceID}, nil)
			if err == nil || !strings.Contains(err.Error(), "reset-id") {
				t.Fatal(err)
			}
		})
	}
}

func TestResetIDRegenerates(t *testing.T) {
	dir, id := pairIdentity(t)
	st, err := store.Open(filepath.Join(dir, "driga.db"), store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	old := st.MachineID()
	st.Tx(context.Background(), func(tx *store.Tx) error {
		if err := tx.UpsertMachine(model.Machine{Hostname: "clone"}); err != nil {
			return err
		}
		_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: "one"}})
		return err
	})
	st.Close()
	SavePeers(filepath.Join(dir, "fleet"), []Peer{{Name: "old"}})
	SaveViewers(filepath.Join(dir, "fleet"), []Viewer{{Name: "old"}})
	if err := ResetID(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	fresh, err := LoadOrCreateIdentity(filepath.Join(dir, "fleet"))
	if err != nil || fresh.DeviceID == id.DeviceID {
		t.Fatal("identity not regenerated", err)
	}
	st, err = store.Open(filepath.Join(dir, "driga.db"), store.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sessions, err := st.Sessions(store.SessionFilter{IncludeEnded: true})
	if err != nil || st.MachineID() == old || fresh.Cert.Leaf.Subject.CommonName != st.MachineID() || len(sessions) != 1 || sessions[0].MachineID != st.MachineID() {
		t.Fatalf("restamp %+v %v", sessions, err)
	}
	peers, _ := LoadPeers(filepath.Join(dir, "fleet"))
	vs, _ := LoadViewers(filepath.Join(dir, "fleet"))
	if len(peers) != 0 || len(vs) != 0 {
		t.Fatal("old trust survived")
	}
}

func TestMITMRelayCodesDiffer(t *testing.T) {
	s, addr, sock := pairServer(t)
	dir, _ := pairIdentity(t)
	_, mitm := pairIdentity(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	peerCode := make(chan string, 1)
	viewerReady := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := Enroll(ctx, sock, EnrollOpts{}, func(code string) bool { peerCode <- code; <-viewerReady; return false })
		done <- err
	}()
	waitEnrollment(t, s)
	upstream, err := dialPair(ctx, addr, mitm, "")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	relay := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req, resp pairMessage
		json.NewDecoder(r.Body).Decode(&req)
		err := upstream.post(ctx, strings.TrimPrefix(r.URL.Path, "/v1/pair/"), req, &resp)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/reveal") {
			resp.MachineID = mitm.Cert.Leaf.Subject.CommonName
		}
		json.NewEncoder(w).Encode(resp)
	})}
	relayDone := make(chan struct{})
	go func() {
		defer close(relayDone)
		relay.Serve(tls.NewListener(ln, PinnedTLS(mitm, func([32]byte) bool { return true })))
	}()
	defer func() { relay.Close(); <-relayDone }()
	var viewerCode string
	_, err = Add(ctx, ln.Addr().String(), AddOpts{DataDir: dir}, func(code string) bool { viewerCode = code; close(viewerReady); return true })
	if err == nil {
		t.Fatal("MITM paired")
	}
	select {
	case code := <-peerCode:
		if code == viewerCode || viewerCode == "" {
			t.Fatalf("MITM codes %s %s", code, viewerCode)
		}
	case <-ctx.Done():
		t.Fatal("peer code missing")
	}
	cancel()
	<-done
}

func TestPairPromptCancellation(t *testing.T) {
	for _, side := range []string{"viewer", "peer"} {
		t.Run(side, func(t *testing.T) {
			s, addr, sock := pairServer(t)
			dir, _ := pairIdentity(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started := make(chan struct{})
			release := make(chan struct{})
			defer close(release)
			prompt := func(which string) func(string) bool {
				return func(string) bool {
					if side == which {
						close(started)
						<-release
					}
					return true
				}
			}
			enrolled := make(chan error, 1)
			added := make(chan error, 1)
			go func() { _, err := Enroll(ctx, sock, EnrollOpts{}, prompt("peer")); enrolled <- err }()
			waitEnrollment(t, s)
			go func() { _, err := Add(ctx, addr, AddOpts{DataDir: dir}, prompt("viewer")); added <- err }()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("prompt not reached")
			}
			cancel()
			for _, done := range []chan error{enrolled, added} {
				select {
				case err := <-done:
					if err == nil {
						t.Error("cancellation succeeded")
					}
				case <-time.After(time.Second):
					t.Error("prompt ignored cancellation")
				}
			}
		})
	}
}

func TestPairMissingConfirmAndTimeout(t *testing.T) {
	s, addr, _ := pairServer(t)
	_, id := pairIdentity(t)
	s.OpenEnrollment(time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, err := dialPair(ctx, addr, id, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var nv [32]byte
	nv[0] = 42
	commit := sha256.Sum256(nv[:])
	var reply pairMessage
	if err := c.post(ctx, "commit", pairMessage{Commit: fmt.Sprintf("%x", commit)}, &reply); err != nil {
		t.Fatal(err)
	}
	if err := c.post(ctx, "reveal", pairMessage{Nonce: fmt.Sprintf("%x", nv)}, &reply); err != nil {
		t.Fatal(err)
	}
	vs, _ := LoadViewers(filepath.Join(s.dataDir, "fleet"))
	if len(vs) != 1 {
		t.Fatal("reveal granted trust")
	}
	s.mu.Lock()
	for _, p := range s.enrollment.pairs {
		p.until = s.now().Add(-time.Second)
	}
	s.mu.Unlock()
	if err := c.post(ctx, "confirm", pairMessage{Name: "late"}, new(Viewer)); err == nil {
		t.Fatal("expired confirmation accepted")
	}
	vs, _ = LoadViewers(filepath.Join(s.dataDir, "fleet"))
	if len(vs) != 1 {
		t.Fatal("timeout granted trust")
	}
}

func TestPairCannotMoveAcrossTLSSessions(t *testing.T) {
	s, addr, _ := pairServer(t)
	_, id := pairIdentity(t)
	s.OpenEnrollment(time.Minute)
	c, err := dialPair(context.Background(), addr, id, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var nv [32]byte
	nv[0] = 42
	commit := sha256.Sum256(nv[:])
	var reply pairMessage
	if err := c.post(context.Background(), "commit", pairMessage{Commit: fmt.Sprintf("%x", commit)}, &reply); err != nil {
		t.Fatal(err)
	}
	other, err := dialPair(context.Background(), addr, id, "")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := other.post(context.Background(), "reveal", pairMessage{Nonce: fmt.Sprintf("%x", nv)}, &reply); err == nil {
		t.Fatal("attempt moved between TLS sessions")
	}
}

func TestPairBeforeFirstPass(t *testing.T) {
	s, addr, sock := startPairServer(t, true)
	dir, id := pairIdentity(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, e := Enroll(ctx, sock, EnrollOpts{AllowID: id.DeviceID}, nil); e != nil {
		t.Fatal(e)
	}
	peer, e := Add(ctx, addr, AddOpts{DataDir: dir, ID: s.id.DeviceID}, nil)
	if e != nil {
		t.Fatal(e)
	}
	host, _ := os.Hostname()
	if peer.MachineID != s.st.MachineID() || peer.Hostname != host || peer.OS != runtime.GOOS {
		t.Fatalf("%+v", peer)
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: PinnedTLS(id, func(fp [32]byte) bool { return EqualFingerprint(fp, sha256.Sum256(s.id.DER)) })}}
	defer client.CloseIdleConnections()
	resp, e := client.Get("https://" + addr + "/v1/hello")
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	var hello map[string]any
	if e = json.NewDecoder(resp.Body).Decode(&hello); e != nil {
		t.Fatal(e)
	}
	if resp.StatusCode != 200 || hello["machine_id"] != s.st.MachineID() || hello["hostname"] != host || hello["os"] != runtime.GOOS || hello["arch"] != runtime.GOARCH {
		t.Fatalf("%d %+v", resp.StatusCode, hello)
	}
}
