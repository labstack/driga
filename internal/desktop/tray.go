package desktop

import (
	"fmt"
	"log/slog"

	"github.com/egoist/mygo"
)

// applyTray creates or removes the tray icon. Without a host for tray icons,
// as on a Linux desktop with no StatusNotifierItem host, there is none, and
// closing the window quits.
func (s *shell) applyTray(on bool) {
	s.mu.Lock()
	tray := s.tray
	s.mu.Unlock()
	switch {
	case on && tray == nil:
		l := trayLook(s.goos, 0, false)
		icon, _ := assets.ReadFile("assets/" + l.Icon)
		created, err := mygo.NewTray(mygo.TrayOptions{ID: "driga", Icon: icon, IconIsTemplate: l.Template, ToolTip: l.ToolTip})
		if err != nil {
			slog.Warn("no tray icon", "event", "tray_unavailable", "err", err.Error())
			break
		}
		created.OnAppearanceChange(func() { s.redrawTray(true) })
		s.mu.Lock()
		s.tray = created
		s.mu.Unlock()
		s.redrawTray(true)
	case !on && tray != nil:
		s.mu.Lock()
		s.tray, s.trayKey = nil, ""
		s.mu.Unlock()
		tray.Destroy()
	}
	s.applyDock()
}

func (s *shell) trayCreated() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tray != nil
}

func (s *shell) updateTray() { s.redrawTray(false) }

// redrawTray sets the tray's icon, title, tooltip and menu from the needs and
// the collector's status. The menu is replaced only when it changed, so one
// the user has open stays put. Status and needs arrive on different
// goroutines; drawing on the main thread keeps an older menu from landing
// after a newer one.
func (s *shell) redrawTray(force bool) {
	if !s.trayCreated() {
		return
	}
	mygo.RunOnMain(func() { s.drawTray(force) })
}

func (s *shell) drawTray(force bool) {
	st := s.trayStatus()
	s.mu.Lock()
	tray, needs := s.tray, s.needs
	s.mu.Unlock()
	if tray == nil {
		return
	}
	model := TrayMenuModel(needs, st)
	l := trayLook(s.goos, len(needs), tray.Dark())
	key := fmt.Sprintf("%v|%+v", model, l)
	s.mu.Lock()
	if !force && key == s.trayKey || s.tray != tray {
		s.mu.Unlock()
		return
	}
	s.trayKey = key
	s.mu.Unlock()
	if icon, err := assets.ReadFile("assets/" + l.Icon); err == nil {
		_ = tray.SetIcon(icon, l.Template)
	}
	tray.SetToolTip(l.ToolTip)
	tray.SetMenu(s.menu(model))
}
