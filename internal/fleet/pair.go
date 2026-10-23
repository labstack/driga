package fleet

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/labstack/driga/internal/config"
)

const confirmTimeout = 2 * time.Minute

// PairCode derives the six-digit confirmation bound to both identities and TLS.
func PairCode(nV, nP, fpV, fpP [32]byte, ekm []byte) string {
	ikm := append(append([]byte{}, nV[:]...), nP[:]...)
	info := append(append(append([]byte{}, fpV[:]...), fpP[:]...), ekm...)
	for size := 32; size <= 255*sha256.Size; size += 32 {
		key, err := hkdf.Key(sha256.New, ikm, []byte("driga-pair-v1"), string(info), size)
		if err != nil {
			panic(err)
		}
		if code, err := samplePairCode(bytes.NewReader(key)); err == nil {
			return code
		}
	}
	panic("pairing HKDF rejection stream exhausted")
}

func samplePairCode(r io.Reader) (string, error) {
	for {
		var b [4]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return "", err
		}
		v := binary.BigEndian.Uint32(b[:])
		if v >= 4294000000 {
			continue
		}
		v %= 1000000
		return fmt.Sprintf("%03d %03d", v/1000, v%1000), nil
	}
}

// EnrollOpts controls a bounded enrollment window and optional identity pin.
type EnrollOpts struct {
	Window  time.Duration
	Ack     bool
	AllowID string
}

// AddOpts controls the viewer's local state, peer name and optional identity pin.
type AddOpts struct {
	DataDir string
	Name    string
	ID      string
}

type enrollment struct {
	token    string
	allow    string
	ack      bool
	attempts int
	seen     map[string]bool
	pairs    map[string]*pairAttempt
	result   *Viewer
	timer    *time.Timer
}
type pairAttempt struct {
	commit, nP, fpV                       [32]byte
	device, machine, remote               string
	code                                  string
	revealed, decided, approved, finished bool
	until                                 time.Time
	done                                  chan struct{}
	timer                                 *time.Timer
}

type pairMessage struct {
	Commit       string `json:"commit,omitempty"`
	Nonce        string `json:"nonce,omitempty"`
	MachineID    string `json:"machine_id,omitempty"`
	Hostname     string `json:"hostname,omitempty"`
	OS           string `json:"os,omitempty"`
	Arch         string `json:"arch,omitempty"`
	DrigaVersion string `json:"driga_version,omitempty"`
	Name         string `json:"name,omitempty"`
	Abort        bool   `json:"abort,omitzero"`
}

func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func signalPair(p *pairAttempt) {
	select {
	case <-p.done:
	default:
		close(p.done)
	}
}

func (s *Server) closeEnrollmentLocked() {
	s.enrollUntil = time.Time{}
	if s.enrollment != nil {
		if s.enrollment.timer != nil {
			s.enrollment.timer.Stop()
		}
		for _, p := range s.enrollment.pairs {
			if p.timer != nil {
				p.timer.Stop()
			}
			if !p.finished {
				p.finished = true
				signalPair(p)
			}
		}
	}
	_ = os.Remove(filepath.Join(s.dataDir, "fleet", "enroll.json"))
}
func (s *Server) expireEnrollmentLocked() {
	if !s.enrollUntil.IsZero() && !s.now().Before(s.enrollUntil) {
		s.closeEnrollmentLocked()
	}
}

func (s *Server) openEnrollment(opts EnrollOpts) (string, error) {
	if opts.AllowID != "" {
		fp, err := ParseDeviceID(opts.AllowID)
		if err != nil {
			return "", err
		}
		opts.AllowID = canonicalDeviceID(fp)
	}
	window := opts.Window
	if window == 0 {
		window = s.cfg.Fleet.EnrollWindow
	}
	if window == 0 {
		window = 10 * time.Minute
	}
	if window <= 0 || window > 10*time.Minute {
		return "", fmt.Errorf("enrollment window must be positive and at most 10m")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireEnrollmentLocked()
	if s.now().Before(s.enrollUntil) {
		return "", fmt.Errorf("enrollment already open")
	}
	until := s.now().Add(window)
	token := randomToken()
	b, err := json.Marshal(struct {
		ExpiresAt time.Time `json:"expires_at"`
		AllowID   string    `json:"allow_id,omitempty"`
		Ack       bool      `json:"ack"`
	}{until.UTC(), opts.AllowID, opts.Ack})
	if err != nil {
		return "", err
	}
	if err := atomicPrivateWrite(filepath.Join(s.dataDir, "fleet", "enroll.json"), append(b, '\n')); err != nil {
		return "", err
	}
	s.enrollment = &enrollment{token: token, allow: opts.AllowID, ack: opts.Ack, seen: map[string]bool{}, pairs: map[string]*pairAttempt{}}
	s.enrollUntil = until
	s.enrollment.timer = time.AfterFunc(window, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.enrollment != nil && s.enrollment.token == token {
			s.expireEnrollmentLocked()
		}
	})
	return token, nil
}

// canonicalDeviceID reconstructs display grouping without hashing a fingerprint.
func canonicalDeviceID(fp [32]byte) string {
	raw := deviceEncoding.EncodeToString(fp[:])
	groups := []string{}
	for i := 0; i < len(raw); i += 13 {
		block := raw[i : i+13]
		block += string(deviceCheck(block))
		groups = append(groups, block[:7], block[7:])
	}
	return strings.Join(groups, "-")
}

func (s *Server) enrollmentAccepts(fp [32]byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireEnrollmentLocked()
	if !s.now().Before(s.enrollUntil) {
		return false
	}
	if s.enrollment.allow == "" {
		return true
	}
	pin, _ := ParseDeviceID(s.enrollment.allow)
	return EqualFingerprint(pin, fp)
}

func decodePair(r *http.Request) (pairMessage, error) {
	var m pairMessage
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return m, err
	}
	if dec.Decode(new(any)) != io.EOF {
		return m, fmt.Errorf("trailing pairing data")
	}
	return m, nil
}
func hex32(v string) (b [32]byte, err error) {
	raw, e := hex.DecodeString(v)
	if e != nil || len(raw) != 32 {
		return b, fmt.Errorf("expected 32-byte hex value")
	}
	copy(b[:], raw)
	return b, nil
}

func (s *Server) failPairLocked(key string) {
	e := s.enrollment
	if e == nil {
		return
	}
	if p := e.pairs[key]; p != nil {
		if p.timer != nil {
			p.timer.Stop()
		}
		if !p.finished {
			p.finished = true
			signalPair(p)
		}
		delete(e.pairs, key)
	}
	if e.attempts >= 3 {
		s.closeEnrollmentLocked()
	}
}

func (s *Server) pairHTTP(w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		jsonError(w, "untrusted viewer", 403)
		return
	}
	fp := sha256.Sum256(r.TLS.PeerCertificates[0].Raw)
	if !s.enrollmentAccepts(fp) {
		jsonError(w, "enrollment closed or identity not allowed", 403)
		return
	}
	if r.TLS.Version != tls.VersionTLS13 || !r.TLS.HandshakeComplete {
		jsonError(w, "TLS 1.3 session required", http.StatusBadRequest)
		return
	}
	ekm, err := r.TLS.ExportKeyingMaterial("EXPORTER-driga-pairing", nil, 32)
	if err != nil {
		jsonError(w, "TLS exporter unavailable", 400)
		return
	}
	key := hex.EncodeToString(ekm)
	msg, decodeErr := decodePair(r)
	s.mu.Lock()
	s.expireEnrollmentLocked()
	e := s.enrollment
	if e == nil || !s.now().Before(s.enrollUntil) {
		s.mu.Unlock()
		jsonError(w, "enrollment closed", 403)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1/pair/")
	if path == "commit" {
		if e.attempts >= 3 {
			s.mu.Unlock()
			jsonError(w, "enrollment closed (3 attempts)", 403)
			return
		}
		e.attempts++
		commit, err := hex32(msg.Commit)
		if decodeErr != nil || err != nil || e.seen[hex.EncodeToString(commit[:])] || e.pairs[key] != nil {
			s.failPairLocked(key)
			s.mu.Unlock()
			jsonError(w, "invalid or replayed commitment", 400)
			return
		}
		e.seen[hex.EncodeToString(commit[:])] = true
		p := &pairAttempt{commit: commit, fpV: fp, device: DeviceID(r.TLS.PeerCertificates[0].Raw), machine: r.TLS.PeerCertificates[0].Subject.CommonName, remote: r.RemoteAddr, done: make(chan struct{}), until: s.now().Add(confirmTimeout)}
		if err := validatePeerMachineID(p.machine); err != nil {
			s.failPairLocked(key)
			s.mu.Unlock()
			jsonError(w, err.Error(), http.StatusConflict)
			return
		}
		if p.machine == s.st.MachineID() {
			s.failPairLocked(key)
			s.mu.Unlock()
			jsonError(w, "local machine and viewer claim the same machine_id; run driga machine reset-id on the clone", 409)
			return
		}
		if _, err := rand.Read(p.nP[:]); err != nil {
			s.failPairLocked(key)
			s.mu.Unlock()
			jsonError(w, "nonce unavailable", 503)
			return
		}
		e.pairs[key] = p
		token := e.token
		p.timer = time.AfterFunc(confirmTimeout, func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.enrollment != nil && s.enrollment.token == token && s.enrollment.pairs[key] == p {
				s.failPairLocked(key)
			}
		})
		commitP := sha256.Sum256(p.nP[:])
		s.mu.Unlock()
		json.NewEncoder(w).Encode(pairMessage{Commit: hex.EncodeToString(commitP[:])})
		return
	}
	p := e.pairs[key]
	if p == nil || p.finished || !EqualFingerprint(p.fpV, fp) || !s.now().Before(p.until) || decodeErr != nil {
		s.failPairLocked(key)
		s.mu.Unlock()
		jsonError(w, "invalid or expired pairing attempt", 400)
		return
	}
	if path == "reveal" {
		nonce, err := hex32(msg.Nonce)
		commit := sha256.Sum256(nonce[:])
		if p.revealed || err != nil || !EqualFingerprint(commit, p.commit) {
			s.failPairLocked(key)
			s.mu.Unlock()
			jsonError(w, "commitment mismatch or replay", 400)
			return
		}
		p.revealed = true
		p.until = s.now().Add(confirmTimeout)
		p.timer.Reset(confirmTimeout)
		p.code = PairCode(nonce, p.nP, fp, sha256.Sum256(s.id.DER), ekm)
		if e.allow != "" {
			p.decided = true
			p.approved = true
			signalPair(p)
		}
		nonceP := hex.EncodeToString(p.nP[:])
		s.mu.Unlock()
		m, err := localMachineIdentity(s.st)
		if err != nil {
			s.mu.Lock()
			s.failPairLocked(key)
			s.mu.Unlock()
			jsonError(w, "machine unavailable", 503)
			return
		}
		json.NewEncoder(w).Encode(pairMessage{Nonce: nonceP, MachineID: s.st.MachineID(), Hostname: m.Hostname, OS: m.OS, Arch: m.Arch, DrigaVersion: m.DrigaVersion})
		return
	}
	if !p.revealed || msg.Abort || msg.Name == "" || len(msg.Name) > 256 {
		s.failPairLocked(key)
		s.mu.Unlock()
		jsonError(w, "pairing declined or invalid confirmation", 400)
		return
	}
	s.mu.Unlock()
	timer := time.NewTimer(confirmTimeout)
	defer timer.Stop()
	select {
	case <-p.done:
	case <-timer.C:
	case <-r.Context().Done():
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireEnrollmentLocked()
	if s.enrollment != e {
		jsonError(w, "enrollment session changed", 403)
		return
	}
	if e.pairs[key] != p || p.finished || !p.approved || !s.now().Before(p.until) || !s.now().Before(s.enrollUntil) || r.Context().Err() != nil {
		s.failPairLocked(key)
		jsonError(w, "pairing declined or confirmation timed out", 403)
		return
	}
	viewer := Viewer{Name: msg.Name, DeviceID: p.device, MachineID: p.machine, Permissions: []string{"read"}, PairedAt: s.now().UTC(), EnrolledFrom: p.remote}
	if e.ack {
		viewer.Permissions = append(viewer.Permissions, "ack")
	}
	trustMu.Lock()
	vs, err := LoadViewers(filepath.Join(s.dataDir, "fleet"))
	if err == nil {
		for _, v := range vs {
			if v.MachineID != "" && v.MachineID == viewer.MachineID && v.DeviceID != viewer.DeviceID {
				err = fmt.Errorf("%s and %s claim the same machine_id; run driga machine reset-id on the clone", v.Name, p.remote)
				break
			}
		}
	}
	if err == nil {
		keep := make([]Viewer, 0, len(vs)+1)
		for _, v := range vs {
			if v.DeviceID != viewer.DeviceID {
				keep = append(keep, v)
			}
		}
		vs = append(keep, viewer)
		err = SaveViewers(filepath.Join(s.dataDir, "fleet"), vs)
	}
	trustMu.Unlock()
	if err != nil {
		s.failPairLocked(key)
		jsonError(w, err.Error(), 409)
		return
	}
	delete(s.revoked, viewer.DeviceID)
	e.result = &viewer
	// The approved signal may already have closed done; mark it before closing
	// the window so cleanup does not close the same channel again.
	p.finished = true
	s.closeEnrollmentLocked()
	json.NewEncoder(w).Encode(viewer)
}

func (s *Server) pairControl(req ControlRequest) (ControlResponse, error) {
	if req.Op == "enroll" {
		var d time.Duration
		var err error
		if req.Window != "" {
			d, err = time.ParseDuration(req.Window)
		}
		if err != nil {
			return ControlResponse{}, err
		}
		token, err := s.openEnrollment(EnrollOpts{Window: d, Ack: req.Ack, AllowID: req.DeviceID})
		return ControlResponse{Token: token}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireEnrollmentLocked()
	e := s.enrollment
	if e == nil || req.Token != e.token {
		return ControlResponse{}, fmt.Errorf("enrollment session changed")
	}
	if req.Op == "enroll_close" {
		s.closeEnrollmentLocked()
		return ControlResponse{}, nil
	}
	if e.result != nil {
		return ControlResponse{Viewer: e.result}, nil
	}
	if !s.now().Before(s.enrollUntil) {
		return ControlResponse{}, fmt.Errorf("enrollment closed (expired or 3 attempts)")
	}
	if req.Op == "enroll_decision" {
		p := e.pairs[req.Attempt]
		if p == nil || !p.revealed || p.decided || p.finished || !s.now().Before(p.until) {
			return ControlResponse{}, fmt.Errorf("pairing attempt expired")
		}
		p.decided = true
		p.approved = req.Approve
		signalPair(p)
		return ControlResponse{}, nil
	}
	for key, p := range e.pairs {
		if p.revealed && !p.decided && !p.finished {
			return ControlResponse{Code: p.code, Attempt: key}, nil
		}
	}
	return ControlResponse{}, nil
}

// Enroll opens the collector's window through its socket. A pinned enrollment
// returns immediately; interactive enrollment waits for both confirmations.
func Enroll(ctx context.Context, dataDir string, opts EnrollOpts, prompt func(string) bool) (Viewer, error) {
	req := ControlRequest{Op: "enroll", DeviceID: opts.AllowID, Ack: opts.Ack}
	if opts.Window != 0 {
		req.Window = opts.Window.String()
	}
	resp, err := Control(ctx, dataDir, req)
	if err != nil {
		return Viewer{}, err
	}
	if opts.AllowID != "" {
		return Viewer{}, nil
	}
	token := resp.Token
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		Control(cleanup, dataDir, ControlRequest{Op: "enroll_close", Token: token})
	}()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		resp, err = Control(ctx, dataDir, ControlRequest{Op: "enroll_poll", Token: token})
		if err != nil {
			return Viewer{}, err
		}
		if resp.Viewer != nil {
			return *resp.Viewer, nil
		}
		if resp.Code != "" {
			yes, promptErr := askPair(ctx, resp.Code, prompt)
			if promptErr != nil {
				return Viewer{}, promptErr
			}
			_, err = Control(ctx, dataDir, ControlRequest{Op: "enroll_decision", Token: token, Attempt: resp.Attempt, Approve: yes})
			if err != nil {
				return Viewer{}, err
			}
			if !yes {
				return Viewer{}, fmt.Errorf("pairing declined")
			}
		}
		select {
		case <-ctx.Done():
			return Viewer{}, ctx.Err()
		case <-tick.C:
		}
	}
}

func askPair(ctx context.Context, code string, prompt func(string) bool) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if prompt == nil {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, confirmTimeout)
	defer cancel()
	answer := make(chan bool, 1)
	go func() { answer <- prompt(code) }()
	select {
	case yes := <-answer:
		return yes, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

type pairConn struct {
	*tls.Conn
	reader *bufio.Reader
	addr   string
}

func dialPair(ctx context.Context, addr string, id Identity, pin string) (*pairConn, error) {
	var expected [32]byte
	var err error
	if pin != "" {
		expected, err = ParseDeviceID(pin)
		if err != nil {
			return nil, err
		}
	}
	cfg := PinnedTLS(id, func(fp [32]byte) bool { return pin == "" || EqualFingerprint(expected, fp) })
	cfg.NextProtos = []string{"http/1.1"}
	raw, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	c := tls.Client(raw, cfg)
	handshake, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.HandshakeContext(handshake); err != nil {
		c.Close()
		return nil, err
	}
	return &pairConn{Conn: c, reader: bufio.NewReader(c), addr: addr}, nil
}
func (c *pairConn) post(ctx context.Context, path string, msg pairMessage, out any) error {
	deadline := time.Now().Add(confirmTimeout + 5*time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	c.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://"+c.addr+"/v1/pair/"+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if err := req.Write(c); err != nil {
		return err
	}
	resp, err := http.ReadResponse(c.reader, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil {
		return err
	}
	if len(body) > 8<<20 {
		return fmt.Errorf("pairing response too large")
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("pairing: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, out)
}

// Add pairs on one TLS connection and saves trust only after both sides agree.
func Add(ctx context.Context, addr string, opts AddOpts, prompt func(string) bool) (Peer, error) {
	var zero Peer
	addr, err := pairAddress(addr)
	if err != nil {
		return zero, err
	}
	if opts.DataDir == "" {
		opts.DataDir = config.DataDir()
	}
	dir := filepath.Join(opts.DataDir, "fleet")
	id, err := LoadOrCreateIdentity(dir)
	if err != nil {
		return zero, err
	}
	peers, err := LoadPeers(dir)
	if err != nil {
		return zero, err
	}
	known := append([]Peer{}, peers...)
	cached, err := cachedPeers(opts.DataDir)
	if err != nil {
		return zero, err
	}
	removed, err := loadTrust[Peer](filepath.Join(dir, "removed-peers.json"))
	if err != nil {
		return zero, err
	}
	for _, p := range cached {
		retired := false
		for _, old := range removed {
			if old.MachineID == p.MachineID && old.DeviceID == p.DeviceID {
				retired = true
				break
			}
		}
		if p.DeviceID != "" && !retired {
			known = append(known, p)
		}
	}
	pin := opts.ID
	for _, p := range peers {
		if p.Addr == addr {
			if p.State == "untrusted" {
				return zero, fmt.Errorf("peer identity changed: untrusted; run driga fleet rekey on the peer (pairings must be redone), then driga fleet remove and pair again")
			}
			if pin != "" {
				a, e := ParseDeviceID(pin)
				b, f := ParseDeviceID(p.DeviceID)
				if e != nil || f != nil || !EqualFingerprint(a, b) {
					return zero, markUntrusted(dir, addr)
				}
			}
			pin = p.DeviceID
		}
	}
	c, err := dialPair(ctx, addr, id, pin)
	if err != nil {
		// Only a certificate verification failure indicates an identity change;
		// network failures must preserve the existing trust state.
		var verify *tls.CertificateVerificationError
		if errors.As(err, &verify) || strings.Contains(err.Error(), "untrusted fleet peer certificate") {
			for _, p := range peers {
				if p.Addr == addr {
					return zero, markUntrusted(dir, addr)
				}
			}
		}
		return zero, err
	}
	defer c.Close()
	cs := c.ConnectionState()
	certP := cs.PeerCertificates[0]
	fpP := sha256.Sum256(certP.Raw)
	var nV [32]byte
	if _, err := rand.Read(nV[:]); err != nil {
		return zero, err
	}
	commit := sha256.Sum256(nV[:])
	var committed, revealed pairMessage
	if err := c.post(ctx, "commit", pairMessage{Commit: hex.EncodeToString(commit[:])}, &committed); err != nil {
		return zero, err
	}
	if err := c.post(ctx, "reveal", pairMessage{Nonce: hex.EncodeToString(nV[:])}, &revealed); err != nil {
		return zero, err
	}
	nP, err := hex32(revealed.Nonce)
	if err != nil {
		return zero, err
	}
	expected, err := hex32(committed.Commit)
	if err != nil || !EqualFingerprint(sha256.Sum256(nP[:]), expected) {
		return zero, fmt.Errorf("peer commitment mismatch")
	}
	if err := validatePeerMachineID(revealed.MachineID); err != nil {
		return zero, err
	}
	if revealed.MachineID != certP.Subject.CommonName {
		return zero, fmt.Errorf("peer machine_id does not match certificate; run driga fleet rekey on the peer (pairings must be redone)")
	}
	if err := checkClone(known, Peer{MachineID: revealed.MachineID, DeviceID: DeviceID(certP.Raw), Addr: addr}, id); err != nil {
		return zero, err
	}
	ekm, err := cs.ExportKeyingMaterial("EXPORTER-driga-pairing", nil, 32)
	if err != nil {
		return zero, err
	}
	code := PairCode(nV, nP, sha256.Sum256(id.DER), fpP, ekm)
	if opts.ID == "" {
		yes, err := askPair(ctx, code, prompt)
		if err != nil {
			return zero, err
		}
		if !yes {
			c.post(ctx, "confirm", pairMessage{Abort: true}, new(Viewer))
			return zero, fmt.Errorf("pairing declined")
		}
	}
	hostname, err := os.Hostname()
	if err != nil {
		return zero, err
	}
	var viewer Viewer
	if err := c.post(ctx, "confirm", pairMessage{Name: hostname}, &viewer); err != nil {
		return zero, err
	}
	if viewer.DeviceID != id.DeviceID || viewer.MachineID != id.Cert.Leaf.Subject.CommonName || !hasPermission(viewer, "read") {
		return zero, fmt.Errorf("invalid pairing confirmation")
	}
	name := opts.Name
	if name == "" {
		name = revealed.Hostname
	}
	if name == "" {
		name = addr
	}
	peer := Peer{Name: name, Addr: addr, DeviceID: DeviceID(certP.Raw), MachineID: revealed.MachineID, Hostname: revealed.Hostname, OS: revealed.OS, PairedAt: time.Now().UTC(), Ack: hasPermission(viewer, "ack")}
	trustMu.Lock()
	defer trustMu.Unlock()
	peers, err = LoadPeers(dir)
	if err != nil {
		return zero, err
	}
	if err := checkClone(peers, peer, id); err != nil {
		return zero, err
	}
	keep := make([]Peer, 0, len(peers)+1)
	for _, p := range peers {
		if p.Addr != addr && p.DeviceID != peer.DeviceID {
			if p.Name == name {
				return zero, fmt.Errorf("peer name %q already exists", name)
			}
			keep = append(keep, p)
		}
	}
	if err := SavePeers(dir, append(keep, peer)); err != nil {
		return zero, err
	}
	return peer, nil
}

func pairAddress(addr string) (string, error) {
	if strings.ContainsAny(addr, "/\\?#@") {
		return "", fmt.Errorf("expected host[:port]")
	}
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr, nil
	}
	if strings.Contains(addr, ":") && net.ParseIP(strings.Trim(addr, "[]")) == nil {
		return "", fmt.Errorf("invalid peer address")
	}
	if addr == "" {
		return "", fmt.Errorf("peer host required")
	}
	return net.JoinHostPort(strings.Trim(addr, "[]"), "7411"), nil
}
func checkClone(peers []Peer, candidate Peer, id Identity) error {
	if candidate.MachineID == id.Cert.Leaf.Subject.CommonName {
		return fmt.Errorf("local machine and %s claim the same machine_id; run driga machine reset-id on the clone", candidate.Addr)
	}
	for _, p := range peers {
		if p.MachineID == candidate.MachineID && p.DeviceID != candidate.DeviceID {
			return fmt.Errorf("%s and %s claim the same machine_id; run driga machine reset-id on the clone", p.Name, candidate.Addr)
		}
	}
	return nil
}
func markUntrusted(dir, addr string) error {
	trustMu.Lock()
	defer trustMu.Unlock()
	ps, err := LoadPeers(dir)
	if err != nil {
		return err
	}
	for i := range ps {
		if ps[i].Addr == addr {
			ps[i].State = "untrusted"
		}
	}
	if err := SavePeers(dir, ps); err != nil {
		return err
	}
	return fmt.Errorf("peer identity changed: untrusted; run driga fleet rekey on the peer (pairings must be redone), then driga fleet remove and pair again")
}
