package chat

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/secfile"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/labstack/driga/internal/store"
)

func SocketPath(dataDir string) string { return fleet.WatchSocketPath(dataDir, "chat") }

// Listen is owned by the collector lock holder. Never replace a live socket or
// a regular file. The private parent prevents access during listen/chmod.
func Listen(ctx context.Context, dataDir string, b *Broker) (func(), error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return listen(ctx, dataDir, b)
}
func listen(ctx context.Context, dataDir string, b *Broker) (func(), error) {
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
	if b.clientToken == "" {
		b.clientToken = uuid.NewString() + uuid.NewString()
	}
	key := b.clientToken
	keyPath := filepath.Join(dataDir, "chat.key")
	f, err := secfile.CreateTemp(dataDir, "chat-key-*")
	if err != nil {
		l.Close()
		return nil, err
	}
	name := f.Name()
	if _, err = f.WriteString(key); err == nil {
		err = f.Close()
	} else {
		f.Close()
	}
	if err == nil {
		err = os.Rename(name, keyPath)
	}
	if err != nil {
		os.Remove(name)
		l.Close()
		return nil, err
	}
	b.socket = path
	server := &http.Server{Handler: handler(b), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: ApprovalTTL + time.Minute, WriteTimeout: ApprovalTTL + time.Minute, IdleTimeout: time.Minute, BaseContext: func(net.Listener) context.Context { return ctx }}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(l) }()
	var once sync.Once
	closeFn := func() { once.Do(func() { _ = server.Close(); <-done; _ = os.Remove(path); _ = os.Remove(keyPath) }) }
	return closeFn, nil
}
func handler(b *Broker) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var out any
		var err error
		bridge := r.URL.Path == "/approve" || r.URL.Path == "/claim" || r.URL.Path == "/complete" || r.URL.Path == "/chat-prompts" || r.URL.Path == "/answer-approved-prompt"
		if !bridge && (b.clientToken == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(b.clientToken)) != 1) {
			http.Error(w, "invalid client token", 403)
			return
		}
		switch {
		case r.Method == "POST" && bridge:
			var in struct {
				Approval
				ID, Result string
				Failed     bool
			}
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxText+8192))
			decoder.DisallowUnknownFields()
			err = decoder.Decode(&in)
			if err == nil {
				switch r.URL.Path {
				case "/approve":
					out, err = b.Approve(r.Context(), in.Approval)
				case "/claim":
					out, err = b.Claim(in.Approval)
				case "/chat-prompts":
					out, err = b.ChatPrompts(in.Approval)
				case "/answer-approved-prompt":
					err = b.AnswerApprovedPrompt(in.Approval, in.ID)
				case "/complete":
					err = b.Complete(in.Approval, in.ID, in.Result, in.Failed)
				}
			}
		case r.Method == "POST" && (r.URL.Path == "/mode" || r.URL.Path == "/decide"):
			var in struct {
				ID, Action string
				Mode       Mode
				Allow      bool
			}
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
			decoder.DisallowUnknownFields()
			err = decoder.Decode(&in)
			if err == nil {
				if r.URL.Path == "/mode" {
					err = b.SetMode(r.Context(), in.ID, in.Mode)
				} else {
					err = b.Decide(r.Context(), in.ID, in.Action, in.Allow)
				}
			}
		case r.Method == "POST" && r.URL.Path == "/hook-prompt":
			var in Prompt
			err = json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxText+8192)).Decode(&in)
			if err == nil {
				out, err = b.WaitPrompt(r.Context(), in)
			}
		case r.Method == "POST" && r.URL.Path == "/hook-end":
			var in struct{ Session, Cwd string }
			err = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&in)
			if err == nil {
				b.EndPrompts(in.Session, in.Cwd)
			}
		case r.Method == "GET" && r.URL.Path == "/prompts":
			out, err = b.Prompts(r.Context())
		case r.Method == "POST" && r.URL.Path == "/prompt-answer":
			var in struct {
				ID    string
				Allow bool
			}
			err = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in)
			if err == nil {
				err = b.AnswerPrompt(r.Context(), in.ID, in.Allow)
			}
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
		case r.Method == "GET" && (r.URL.Path == "/state" || r.URL.Path == "/wait"):
			var seq uint64
			if raw := r.URL.Query().Get("after"); raw != "" {
				_, err = fmt.Sscan(raw, &seq)
			}
			if err == nil {
				if r.URL.Path == "/wait" {
					ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
					out, err = b.Wait(ctx, r.URL.Query().Get("id"), seq)
					cancel()
					if errors.Is(err, context.DeadlineExceeded) && r.Context().Err() == nil {
						out, err = b.Read(r.Context(), r.URL.Query().Get("id"), seq)
					}
				} else {
					out, err = b.Read(r.Context(), r.URL.Query().Get("id"), seq)
				}
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
	mu        sync.Mutex
	fullAuto  map[string]bool
	token     string
	keyPath   string
	http      *http.Client
	transport *http.Transport
}

func Dial(ctx context.Context, dataDir string) (*Remote, error) {
	path := SocketPath(dataDir)
	c, err := dialPath(ctx, path)
	if err != nil {
		return nil, err
	}
	c.keyPath = filepath.Join(dataDir, "chat.key")
	key, err := os.ReadFile(c.keyPath)
	if err == nil {
		c.token = string(key)
		err = c.call(ctx, "GET", "/health", nil, nil)
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}
func dialPath(ctx context.Context, path string) (*Remote, error) {
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
	c := &Remote{http: &http.Client{Transport: t, Timeout: ApprovalTTL + time.Minute}, transport: t}
	return c, nil
}
func (c *Remote) Close() {
	c.mu.Lock()
	ids := c.fullAuto
	c.fullAuto = nil
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for id := range ids {
		_ = c.call(ctx, "POST", "/mode", map[string]any{"id": id, "mode": Confirm}, nil)
	}
	c.transport.CloseIdleConnections()
}
func (c *Remote) call(ctx context.Context, method, path string, in, out any) error {
	return c.callOnce(ctx, method, path, in, out, true)
}
func (c *Remote) callOnce(ctx context.Context, method, path string, in, out any, retry bool) error {
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
	c.mu.Lock()
	token := c.token
	c.mu.Unlock()
	req.Header.Set("Authorization", token)
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	reader := io.LimitReader(res.Body, 40<<20)
	if res.StatusCode != 200 {
		data, _ := io.ReadAll(reader)
		// A rejected client token has not reached a handler, so retry once
		// with the collector's new owner-only key, including for POSTs.
		if res.StatusCode == http.StatusForbidden && retry && c.keyPath != "" {
			key, err := os.ReadFile(c.keyPath)
			if err != nil {
				return err
			}
			if string(key) != token {
				c.mu.Lock()
				c.token = string(key)
				c.fullAuto = nil
				c.mu.Unlock()
				res.Body.Close()
				return c.callOnce(ctx, method, path, in, out, false)
			}
		}
		var e struct{ Error string }
		_ = json.Unmarshal(data, &e)
		if e.Error == "" {
			e.Error = strings.TrimSpace(string(data))
		}
		return fmt.Errorf("chat broker: HTTP %d: %s", res.StatusCode, e.Error)
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
	private, err := os.MkdirTemp("", "driga-chat-")
	if err != nil {
		b.Close()
		if collected != nil {
			collected.Close()
		}
		st.Close()
		return nil, false, nil, err
	}
	stopSocket, err := Listen(ctx, private, b)
	if err != nil {
		os.RemoveAll(private)
		b.Close()
		if collected != nil {
			collected.Close()
		}
		st.Close()
		return nil, false, nil, err
	}
	return b, true, func() {
		b.Close()
		stopSocket()
		os.RemoveAll(private)
		if collected != nil {
			_ = collected.Close()
		}
		_ = st.Close()
	}, nil
}

func (c *Remote) SetMode(ctx context.Context, id string, mode Mode) error {
	err := c.call(ctx, "POST", "/mode", map[string]any{"id": id, "mode": mode}, nil)
	if err == nil {
		c.mu.Lock()
		if mode == FullAuto {
			if c.fullAuto == nil {
				c.fullAuto = map[string]bool{}
			}
			c.fullAuto[id] = true
		} else {
			delete(c.fullAuto, id)
		}
		c.mu.Unlock()
	}
	return err
}
func (c *Remote) Decide(ctx context.Context, id, action string, allow bool) error {
	return c.call(ctx, "POST", "/decide", map[string]any{"id": id, "action": action, "allow": allow}, nil)
}
func Bridge(ctx context.Context, path string, in Approval) (Permission, error) {
	c, err := dialPath(ctx, path)
	if err != nil {
		return Permission{}, err
	}
	defer c.Close()
	var out Permission
	err = c.call(ctx, "POST", "/approve", in, &out)
	return out, err
}
func ApproveFromEnvironment(ctx context.Context, in Approval) (Permission, error) {
	in.Conversation, in.Token = os.Getenv("DRIGA_CHAT_CONVERSATION"), os.Getenv("DRIGA_CHAT_TOKEN")
	return Bridge(ctx, os.Getenv("DRIGA_CHAT_SOCKET"), in)
}
func Claim(ctx context.Context, path string, in Approval) (string, error) {
	c, err := dialPath(ctx, path)
	if err != nil {
		return "", err
	}
	defer c.Close()
	var out string
	err = c.call(ctx, "POST", "/claim", in, &out)
	return out, err
}
func Complete(ctx context.Context, path string, in Approval, id, result string, failed bool) error {
	c, err := dialPath(ctx, path)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.call(ctx, "POST", "/complete", struct {
		Approval
		ID, Result string
		Failed     bool
	}{in, id, result, failed}, nil)
}

func (b *Broker) ensureSocket() error {
	if b.socket != "" {
		return nil
	}
	dir, err := os.MkdirTemp("", "driga-chat-")
	if err != nil {
		return err
	}
	closeSocket, err := listen(b.ctx, dir, b)
	if err != nil {
		os.RemoveAll(dir)
		return err
	}
	b.closeSocket = func() { closeSocket(); _ = os.RemoveAll(dir) }
	return nil
}

func (c *Remote) Wait(ctx context.Context, id string, after uint64) (State, error) {
	var out State
	err := c.call(ctx, "GET", fmt.Sprintf("/wait?id=%s&after=%d", id, after), nil, &out)
	return out, err
}
