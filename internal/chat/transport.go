package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/secfile"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/labstack/driga/internal/store"
)

func SocketPath(dataDir string) string { return fleet.WatchSocketPath(dataDir, "chat") }

// Listen is owned by the collector lock holder. Never replace a live socket or
// a regular file. The private parent prevents access during listen/chmod.
func Listen(ctx context.Context, dataDir string, b *Broker) (func(), error) {
	if err := secfile.MkdirPrivate(dataDir); err != nil {
		return nil, err
	}
	if err := secfile.Protect(dataDir); err != nil {
		return nil, err
	}
	path := SocketPath(dataDir)
	if err := fleet.EnsureSocketDir(path); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("chat socket path is not a socket")
		}
		conn, err := net.DialTimeout("unix", path, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil, errors.New("chat broker already running")
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	// AF_UNIX access on Windows is guarded by the private parent DACL.
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0600); err != nil {
			l.Close()
			return nil, err
		}
	}
	server := &http.Server{Handler: handler(b), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: time.Minute, BaseContext: func(net.Listener) context.Context { return ctx }}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(l) }()
	closeFn := func() { _ = server.Close(); <-done }
	return closeFn, nil
}
func handler(b Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var out any
		var err error
		switch {
		case r.Method == "GET" && r.URL.Path == "/health":
			out = map[string]bool{"ok": true}
		case r.Method == "GET" && r.URL.Path == "/conversations":
			out, err = b.List(r.Context())
		case r.Method == "POST" && r.URL.Path == "/conversations":
			out, err = b.Create(r.Context())
		case r.Method == "POST" && r.URL.Path == "/send":
			var in struct {
				ID, Text string
				Context  Context
			}
			err = json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxText+8192)).Decode(&in)
			if err == nil {
				err = b.Send(r.Context(), in.ID, in.Text, in.Context)
			}
		case r.Method == "POST" && r.URL.Path == "/stop":
			var in struct{ ID string }
			err = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in)
			if err == nil {
				err = b.Stop(r.Context(), in.ID)
			}
		case r.Method == "GET" && r.URL.Path == "/state":
			var seq uint64
			if raw := r.URL.Query().Get("after"); raw != "" {
				_, err = fmt.Sscan(raw, &seq)
			}
			if err == nil {
				out, err = b.Read(r.Context(), r.URL.Query().Get("id"), seq)
			}
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	})
}

type Remote struct {
	http      *http.Client
	transport *http.Transport
}

func Dial(ctx context.Context, dataDir string) (*Remote, error) {
	path := SocketPath(dataDir)
	if err := fleet.CheckSocketDir(path); err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSocket == 0 || runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("chat socket must be owner-only")
	}
	t := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", path)
	}}
	c := &Remote{http: &http.Client{Transport: t, Timeout: 10 * time.Second}, transport: t}
	if err := c.call(ctx, "GET", "/health", nil, nil); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}
func (c *Remote) Close() { c.transport.CloseIdleConnections() }
func (c *Remote) call(ctx context.Context, method, path string, in, out any) error {
	var body bytes.Buffer
	if in != nil {
		if err := json.NewEncoder(&body).Encode(in); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://driga"+path, &body)
	if err != nil {
		return err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	reader := io.LimitReader(res.Body, 40<<20)
	if res.StatusCode != 200 {
		var e struct{ Error string }
		_ = json.NewDecoder(reader).Decode(&e)
		return fmt.Errorf("chat broker: %s", e.Error)
	}
	if out == nil {
		_, err = io.Copy(io.Discard, reader)
		return err
	}
	return json.NewDecoder(reader).Decode(out)
}
func (c *Remote) Create(ctx context.Context) (store.Conversation, error) {
	var out store.Conversation
	err := c.call(ctx, "POST", "/conversations", nil, &out)
	return out, err
}
func (c *Remote) List(ctx context.Context) ([]store.Conversation, error) {
	var out []store.Conversation
	err := c.call(ctx, "GET", "/conversations", nil, &out)
	return out, err
}
func (c *Remote) Send(ctx context.Context, id, text string, scope Context) error {
	return c.call(ctx, "POST", "/send", map[string]any{"id": id, "text": text, "context": scope}, nil)
}
func (c *Remote) Stop(ctx context.Context, id string) error {
	return c.call(ctx, "POST", "/stop", map[string]string{"id": id}, nil)
}
func (c *Remote) Read(ctx context.Context, id string, after uint64) (State, error) {
	var out State
	err := c.call(ctx, "GET", fmt.Sprintf("/state?id=%s&after=%d", id, after), nil, &out)
	return out, err
}

// Open uses the service when available. The fallback is private to the opening
// client, with no persistent driga history or competing collector socket.
func Open(ctx context.Context, dataDir string, snippets bool) (Client, bool, func(), error) {
	remote, err := Dial(ctx, dataDir)
	if err == nil {
		return remote, false, remote.Close, nil
	}
	// Permission and invalid-path failures must not silently select another broker.
	if !errors.Is(err, os.ErrNotExist) && !strings.Contains(err.Error(), "connection refused") {
		return nil, false, nil, err
	}
	st, err := store.Open("", store.Memory)
	if err != nil {
		return nil, false, nil, err
	}
	collected, _ := store.Open(filepath.Join(dataDir, "driga.db"), store.ReadOnly)
	b, err := New(ctx, st, &Claude{DataDir: dataDir}, Options{Snippets: snippets, Ephemeral: true, DataDir: dataDir, ContextStore: collected})
	if err != nil {
		if collected != nil {
			_ = collected.Close()
		}
		st.Close()
		return nil, false, nil, err
	}
	return b, true, func() {
		b.Close()
		if collected != nil {
			_ = collected.Close()
		}
		_ = st.Close()
	}, nil
}
