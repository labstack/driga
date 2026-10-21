package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/web"
)

// WebCmd serves the browser viewer in its own process.
type WebCmd struct {
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
	dashboard, err := web.New(source, web.Options{Chat: chatClient, ChatEphemeral: ephemeral, Host: listener.Addr().String(), Machine: g.Machine, CleanupList: func(ctx context.Context) ([]cleanup.Candidate, error) {
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
			return err
		}
	}
	server := &http.Server{Handler: dashboard, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: time.Minute, BaseContext: func(net.Listener) context.Context { return ctx }}
	stopClose := context.AfterFunc(ctx, func() { _ = server.Close() })
	defer stopClose()
	watchDone := make(chan struct{})
	go func() { defer close(watchDone); dashboard.Watch(ctx) }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			_ = touchHeartbeat()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	err = server.Serve(listener)
	stopped := ctx.Err() != nil
	cancel()
	<-done
	<-watchDone
	if errors.Is(err, http.ErrServerClosed) || stopped {
		return nil
	}
	return err
}

func openWebBrowser(ctx context.Context, address string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.CommandContext(ctx, name, address).Run()
}
