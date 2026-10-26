package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/web"
)

// DevServeCmd serves the app's page and its API over HTTP on loopback, so the
// UI can be developed in a browser with Vite's hot reload in front (just
// dev). It is the app without its window: the same page, data, Chat and
// actions, but no native dialogs, menus, tray or notifications.
type DevServeCmd struct {
	Addr string `default:"127.0.0.1:47801" help:"Loopback address to listen on."`
}

func (c DevServeCmd) Run(g *Globals) error {
	host, _, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return invalidUsage("--addr must be host:port")
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return invalidUsage("--addr must be a loopback address: the page acts on this machine")
	}
	cfg, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	ctx, stop := notifyContext(context.Background())
	defer stop()
	g.Machine = "all"
	local := *g
	local.Machine = "local"
	source, _, closeSource, err := openViewer(ctx, g, cfg, view.Filter{})
	if err != nil {
		return err
	}
	defer closeSource()
	opts := pageOptions(g, &local)
	// Chat goes to the Driga service when it runs, as the app's does.
	var closeChat func()
	if opts.Chat, _, closeChat, err = chat.Open(ctx, config.DataDir(), cfg.CaptureSnippets()); err != nil {
		return err
	}
	defer closeChat()
	page, err := web.New(source, opts)
	if err != nil {
		return err
	}
	go page.Watch(ctx)
	server := &http.Server{Addr: c.Addr, Handler: page, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Fprintf(commandOutput(g), "Serving the app's page at http://%s/app/ from %s\n", c.Addr, config.DataDir())
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
