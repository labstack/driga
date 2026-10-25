package cli

import (
	"time"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/adapter/claude"
	"github.com/labstack/driga/internal/adapter/codex"
	"github.com/labstack/driga/internal/adapter/gemini"
	"github.com/labstack/driga/internal/attention"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/gitscan"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/signatures"
)

var newInboxRunner = func() attention.Runner { return attention.CommandRunner{} }
var nativeToast = attention.WindowsToast

// NewDeps builds the production sources shared by collector and snapshot commands.
func NewDeps(cfg config.Config) (collector.Deps, error) {
	return newDeps(cfg, nil)
}

func newDeps(cfg config.Config, notifier attention.Notifier) (collector.Deps, error) {
	sigs, err := signatures.Load(cfg.ExtraSignatures)
	if err != nil {
		return collector.Deps{}, err
	}
	runner := newInboxRunner()
	// Without a notifier, leave alerts unconsumed for a process that can post.
	if notifier == nil && attention.DesktopNotificationsReady() {
		notifier = attention.DesktopNotifier{Runner: runner, Toast: nativeToast, Mac: attention.MacDesktopNotifier()}
	}
	return collector.Deps{Cfg: cfg, Plat: platform.New(), Adapters: []adapter.Adapter{claude.New(), codex.New(), gemini.New()}, Sigs: sigs, Git: gitscan.NewScanner(4), SpoolDir: config.SpoolDir(), Now: time.Now, State: collector.NewState(), InboxRunner: runner, Notifier: notifier}, nil
}
