package desktop

import (
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/labstack/driga/internal/config"
)

var themeLabels = []string{"System", "Light", "Dark"}

// settingsView is the Settings window: the app's three settings, written to
// the config file the CLI reads and applied at once.
type settingsView struct {
	s           *shell
	theme       string
	tray, login bool
	err         string
}

func (s *shell) openSettings() {
	s.mu.Lock()
	win, cfg := s.settings, s.cfg
	s.mu.Unlock()
	if win != nil {
		win.Show()
		win.Focus()
		return
	}
	theme := "System"
	for _, label := range themeLabels {
		if strings.ToLower(label) == cfg.UI.Theme {
			theme = label
		}
	}
	v := &settingsView{s: s, theme: theme, tray: cfg.App.Tray, login: cfg.App.LaunchAtLogin}
	win = mygo.NewWindow(mygo.WindowOptions{
		Title: "Settings", Width: 460, Height: 240, StateKey: "settings",
		DisableResize: true, DisableMinimize: true, DisableMaximize: true, Content: ui.View(v.view),
	})
	win.OnClosed(func() {
		s.mu.Lock()
		s.settings = nil
		s.mu.Unlock()
	})
	s.mu.Lock()
	s.settings = win
	s.mu.Unlock()
	mygo.App.Focus()
}

func (v *settingsView) view(c *ui.Context) {
	trayLabel := "Show in the notification area"
	if v.s.goos == "darwin" {
		trayLabel = "Show in the menu bar"
	}
	ui.Column(c).Fill().Padding(24).Gap(16).Children(func() {
		ui.Form(c, func() {
			ui.Field(c, "Appearance", func() {
				previous := v.theme
				if ui.Select(c, &v.theme, themeLabels).Changed() {
					theme := strings.ToLower(v.theme)
					if v.save("ui.theme", theme) {
						mygo.Theme.SetSource(themeSource(theme))
					} else {
						v.theme = previous
					}
				}
			})
			ui.Field(c, trayLabel, func() {
				if ui.Switch(c, &v.tray).Changed() {
					if v.save("app.tray", v.tray) {
						v.s.applyTray(v.tray)
					} else {
						v.tray = !v.tray
					}
				}
			}).Description("Closing the window keeps Driga collecting there.")
			ui.Field(c, "Open at login", func() {
				if ui.Switch(c, &v.login).Changed() {
					if !v.save("app.launch_at_login", v.login) {
						v.login = !v.login
					} else if err := v.s.applyOpenAtLogin(v.login); err != nil {
						v.err = "Open at login: " + err.Error()
					}
				}
			})
		})
		if v.err != "" {
			ui.Text(c, v.err).TextColor(c.Theme().Danger)
		}
	})
}

// save writes one setting and keeps the shell's copy of the config current.
func (v *settingsView) save(key string, value any) bool {
	if err := config.Set(config.Path(), map[string]any{key: value}); err != nil {
		v.err = err.Error()
		return false
	}
	v.err = ""
	v.s.mu.Lock()
	switch key {
	case "ui.theme":
		v.s.cfg.UI.Theme = value.(string)
	case "app.tray":
		v.s.cfg.App.Tray = value.(bool)
	case "app.launch_at_login":
		v.s.cfg.App.LaunchAtLogin = value.(bool)
	}
	v.s.mu.Unlock()
	return true
}
