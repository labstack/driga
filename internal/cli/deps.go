package cli

import (
	"os"
	"time"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/adapter/claude"
	"github.com/labstack/driga/internal/adapter/codex"
	"github.com/labstack/driga/internal/adapter/gemini"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/gitscan"
	"github.com/labstack/driga/internal/inbox"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/signatures"
)

var newInboxRunner = func() inbox.Runner { return inbox.CommandRunner{} }
var nativeToast = inbox.WindowsToast

// NewDeps builds the production sources shared by collector and snapshot commands.
func NewDeps(cfg config.Config) (collector.Deps, error) {
	sigs, err := signatures.Load(cfg.ExtraSignatures)
	if err != nil {
		return collector.Deps{}, err
	}
	runner := newInboxRunner()
	exe, err := os.Executable()
	if err != nil {
		return collector.Deps{}, err
	}
	return collector.Deps{Cfg: cfg, Plat: platform.New(), Adapters: []adapter.Adapter{claude.New(), codex.New(), gemini.New()}, Sigs: sigs, Git: gitscan.NewScanner(4), SpoolDir: config.SpoolDir(), Now: time.Now, State: collector.NewState(), InboxRunner: runner, Notifier: inbox.DesktopNotifier{Runner: runner, DrigaPath: stablePath(exe), Toast: nativeToast}}, nil
}
