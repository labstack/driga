package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/web"
)

// WebCmd serves the browser viewer in its own process.
type WebCmd struct {
	WebOptions
	Listen string `default:"127.0.0.1:0" help:"Loopback address and port for the dashboard."`
	Open   bool   `help:"Open the dashboard in your browser."`
}

func (c WebCmd) Run(g *Globals) error {
	ctx, stop := notifyContext(context.Background())
	defer stop()
	return c.run(ctx, g)
}

func (c WebCmd) run(ctx context.Context, g *Globals) error {
	listener, err := web.Listen(c.Listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	cfg, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	source, mode, closeSource, err := openViewer(ctx, g, cfg, view.Filter{})
	if err != nil {
		return err
	}
	defer closeSource()
	if mode == "snapshot" && g.Stderr != nil {
		fmt.Fprintln(g.Stderr, "Collecting a snapshot (no collector running)…")
	}
	local := *g
	local.Machine = "local"
	chatClient, ephemeral, closeChat, err := chat.Open(ctx, config.DataDir(), cfg.CaptureSnippets())
	if err != nil {
		return err
	}
	defer closeChat()
	if ephemeral && g.Stderr != nil {
		fmt.Fprintln(g.Stderr, "Chat runs in-process; conversations end with it.")
	}
	dashboard, err := web.New(source, web.Options{SessionAction: func(ctx context.Context, action string, id int64) error {
		raw, _ := json.Marshal(map[string]any{"session_id": id})
		actionGlobals := local
		actionGlobals.Stdout = io.Discard
		return runChatAction(ctx, &actionGlobals, action, raw)
	}, Chat: chatClient, ChatEphemeral: ephemeral, Host: listener.Addr().String(), Machine: g.Machine, Activity: webHeartbeat, CleanupList: func(ctx context.Context) ([]cleanup.Candidate, error) {
		envelope, err := cleanupRead(ctx, &local, "", false, true)
		if err != nil {
			return nil, err
		}
		return envelope.Data.([]cleanup.Candidate), nil
	}})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(commandOutput(g), dashboard.URL()); err != nil {
		return err
	}
	if c.Open {
		if err := openWebBrowser(ctx, dashboard.URL()); err != nil {
			if g.Stderr != nil {
				fmt.Fprintln(g.Stderr, "driga: could not open browser:", err, "— open the URL above manually")
			}
		}
	}
	server := &http.Server{Handler: dashboard, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: time.Minute, BaseContext: func(net.Listener) context.Context { return ctx }}
	stopClose := context.AfterFunc(ctx, func() { _ = server.Close() })
	defer stopClose()
	watchDone := make(chan struct{})
	go func() { defer close(watchDone); dashboard.Watch(ctx) }()
	err = server.Serve(listener)
	stopped := ctx.Err() != nil
	cancel()
	<-watchDone
	if errors.Is(err, http.ErrServerClosed) || stopped {
		return nil
	}
	return err
}

func webHeartbeat(active bool) {
	if active {
		_ = touchHeartbeat()
	}
}
