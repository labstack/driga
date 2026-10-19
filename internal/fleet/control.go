package fleet

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ControlRequest is one JSON line sent to fleet/fleet.sock. Pairing can extend
// this protocol with code/confirmation messages without exposing a network API.
type ControlRequest struct {
	Op        string `json:"op"`
	DeviceID  string `json:"device_id,omitempty"`
	Window    string `json:"window,omitempty"`
	Ack       bool   `json:"ack,omitempty"`
	Token     string `json:"token,omitempty"`
	Attempt   string `json:"attempt,omitempty"`
	Approve   bool   `json:"approve,omitempty"`
	MachineID string `json:"machine_id,omitempty"`
}

// Status describes the serving collector's current connections and enrollment.
type Status struct {
	DeviceID       string `json:"device_id"`
	Listen         string `json:"listen"`
	Streams        int    `json:"streams"`
	Presence       int    `json:"presence"`
	Connections    int    `json:"connections"`
	Resnapshots    uint64 `json:"resnapshots"`
	EnrollmentOpen bool   `json:"enrollment_open"`
}

// ControlResponse is one response line; errors never terminate other clients.
type ControlResponse struct {
	Status  *Status `json:"status,omitempty"`
	OK      bool    `json:"ok"`
	Error   string  `json:"error,omitempty"`
	Token   string  `json:"token,omitempty"`
	Attempt string  `json:"attempt,omitempty"`
	Code    string  `json:"code,omitempty"`
	Viewer  *Viewer `json:"viewer,omitempty"`
}

// OpenEnrollment opens an interactive enrollment window through local control.
func (s *Server) OpenEnrollment(window time.Duration) error {
	_, err := s.openEnrollment(EnrollOpts{Window: window})
	return err
}

// Status returns a consistent count of connections and streams.
func (s *Server) Status() Status {
	s.mu.Lock()
	s.expireEnrollmentLocked()
	v := Status{DeviceID: s.id.DeviceID, Streams: len(s.streams), Connections: len(s.conns), Resnapshots: s.resnapshots, EnrollmentOpen: s.now().Before(s.enrollUntil)}
	for stream := range s.streams {
		if stream.presence {
			v.Presence++
		}
	}
	s.mu.Unlock()
	if b, err := os.ReadFile(filepath.Join(s.dataDir, "fleet", "listen")); err == nil {
		v.Listen = string(b)
		for len(v.Listen) > 0 && v.Listen[len(v.Listen)-1] == '\n' {
			v.Listen = v.Listen[:len(v.Listen)-1]
		}
	}
	return v
}

func (s *Server) listenControl(ctx context.Context) (net.Listener, error) {
	path := ControlSocketPath(s.dataDir)
	if err := ensureSocketDir(path); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("fleet control path is not a socket")
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	raw, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	ln := &controlListener{Listener: raw, done: make(chan struct{})}
	if err := os.Chmod(path, 0600); err != nil {
		raw.Close()
		return nil, err
	}
	go func() { <-ctx.Done(); _ = ln.Close() }()
	go func() {
		defer close(ln.done)
		var mu sync.Mutex
		conns := map[net.Conn]struct{}{}
		var wg sync.WaitGroup
		defer func() {
			mu.Lock()
			for c := range conns {
				_ = c.Close()
			}
			mu.Unlock()
			wg.Wait()
			s.mu.Lock()
			s.closeEnrollmentLocked()
			s.mu.Unlock()
		}()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			if len(conns) >= 16 {
				mu.Unlock()
				c.Close()
				continue
			}
			conns[c] = struct{}{}
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer c.Close()
				defer func() { mu.Lock(); delete(conns, c); mu.Unlock() }()
				scanner := bufio.NewScanner(c)
				scanner.Buffer(make([]byte, 4096), 8<<20)
				encoder := json.NewEncoder(c)
				for {
					_ = c.SetDeadline(time.Now().Add(5 * time.Second))
					if !scanner.Scan() {
						return
					}
					var req ControlRequest
					var resp ControlResponse
					if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
						resp.Error = "invalid control request"
					} else {
						var err error
						switch req.Op {
						case "status":
							v := s.Status()
							resp.Status = &v
						case "enroll", "enroll_poll", "enroll_decision", "enroll_close":
							resp, err = s.pairControl(req)
						case "revoke":
							err = s.revoke(req.DeviceID)
						case "purge":
							err = s.requestPurge(ctx, req.MachineID)
						default:
							err = fmt.Errorf("unknown control operation %q", req.Op)
						}
						if err != nil {
							resp.Error = err.Error()
						} else {
							resp.OK = true
						}
					}
					if encoder.Encode(resp) != nil {
						return
					}
				}
			}()
		}
	}()
	return ln, nil
}

type controlListener struct {
	net.Listener
	done chan struct{}
	once sync.Once
	err  error
}

func (l *controlListener) Close() error {
	l.once.Do(func() { l.err = l.Listener.Close() })
	<-l.done
	return l.err
}

// Control sends one local command and reads its response. It never starts a
// collector and reports a missing control socket directly to the caller.
func Control(ctx context.Context, dataDir string, req ControlRequest) (ControlResponse, error) {
	var resp ControlResponse
	sock := ControlSocketPath(dataDir)
	if err := checkControlSocket(sock); err != nil {
		return resp, err
	}
	c, err := (&net.Dialer{}).DialContext(ctx, "unix", sock)
	if err != nil {
		return resp, err
	}
	defer c.Close()
	if err := checkControlPeer(c.(*net.UnixConn)); err != nil {
		return resp, err
	}
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	deadline := time.Now().Add(5 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = c.SetDeadline(deadline)
	if err := json.NewEncoder(c).Encode(req); err != nil {
		return resp, err
	}
	scanner := bufio.NewScanner(c)
	scanner.Buffer(make([]byte, 4096), 8<<20)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return resp, err
		}
		return resp, fmt.Errorf("fleet control closed without response")
	}
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		return resp, err
	}
	if resp.Error != "" {
		return resp, fmt.Errorf("fleet control: %s", resp.Error)
	}
	return resp, nil
}

// StartControl opens only the local control socket for a viewing collector.
func (s *Server) StartControl(ctx context.Context) (func(), error) {
	runCtx, cancel := context.WithCancel(ctx)
	ln, err := s.listenControl(runCtx)
	if err != nil {
		cancel()
		return nil, err
	}
	return func() { cancel(); ln.Close() }, nil
}

// NoteResnapshot includes viewer recovery in the local fleet status counter.
func (s *Server) NoteResnapshot() {
	s.mu.Lock()
	s.resnapshots++
	s.mu.Unlock()
}
