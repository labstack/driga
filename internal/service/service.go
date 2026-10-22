// Package service configures the optional per-user collector.
package service

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	ks "github.com/kardianos/service"
	"github.com/labstack/driga/internal/config"
)

// LogPath is shared by launchd output and the service logs command.
func LogPath() string { return logPathForOS(runtime.GOOS) }

func logPathForOS(goos string) string {
	if goos == "windows" {
		return filepath.Join(config.DataDir(), "logs", "collector.log")
	}
	if goos == "linux" {
		root := os.Getenv("XDG_STATE_HOME")
		if !filepath.IsAbs(root) {
			root = filepath.Join(config.ExpandHome("~"), ".local/state")
		}
		return filepath.Join(root, "driga/collector.log")
	}
	return filepath.Join(config.ExpandHome("~"), "Library", "Logs", "driga", "collector.log")
}

// Config returns configuration only; it never contacts the service manager.
func Config(binPath string) *ks.Config { return configForOS(binPath, runtime.GOOS) }

func configForOS(binPath, goos string) *ks.Config {
	// Darwin's LogDirectory option fixes the directory but not the filenames.
	// LaunchdConfig supports directing both streams to the single documented file.
	escape := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;", "{", "&#123;")
	template := strings.ReplaceAll(launchdConfig, "LOG_PATH", escape.Replace(logPathForOS(goos)))
	env := map[string]string{"HOME": config.ExpandHome("~")}
	// PATH and TMUX_TMPDIR come from the installing shell: launchd and systemd
	// start services with a bare PATH, so tmux and agent CLIs would not be found.
	keys := []string{"PATH", "TMUX_TMPDIR", "DRIGA_CONFIG", "DRIGA_DATA_DIR", "DRIGA_SPOOL_DIR", "CODEX_HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS"}
	for _, setting := range config.Settings() {
		if name := setting.EnvName(); name != "" {
			keys = append(keys, name)
		}
	}
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			env[key] = value
		}
	}
	c := &ks.Config{
		Name: "run.driga.collector", DisplayName: "Driga collector",
		Executable: binPath, Arguments: []string{"collect"}, EnvVars: env,
		Option: ks.KeyValue{"UserService": true, "KeepAlive": true, "RunAtLoad": true,
			"LogDirectory": filepath.Dir(logPathForOS(goos)), "LaunchdConfig": template},
	}
	if goos == "linux" {
		c.Option = ks.KeyValue{"UserService": true, "Restart": "always", "LogOutput": false, "SystemdScript": systemdConfig}
	}
	return c
}

// InstallHint explains how a Linux user service can survive logout.
func InstallHint(goos string) string {
	if goos == "linux" {
		return "For collection after logout: loginctl enable-linger $USER"
	}
	return ""
}

// New creates a control handle; installation and startup remain explicit actions.
type Controller interface {
	Install() error
	Uninstall() error
	Start() error
	Stop() error
	Restart() error
	Status() (ks.Status, error)
}

func New(binPath string) (Controller, error) {
	if runtime.GOOS == "windows" {
		return newWindowsService(binPath)
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return nil, errors.New("driga service requires macOS launchd or Linux systemd")
	}
	if !filepath.IsAbs(binPath) {
		return nil, errors.New("service binary path must be absolute")
	}
	return ks.New(controlOnly{}, Config(binPath))
}

type controlOnly struct{}

func (controlOnly) Start(ks.Service) error {
	return errors.New("run driga collect to execute the collector")
}
func (controlOnly) Stop(ks.Service) error { return nil }

const launchdConfig = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>{{Name | html}}</string>
<key>ProgramArguments</key><array>
<string>{{Path | html}}</string>
{{range Arguments}}<string>{{. | html}}</string>
{{end}}</array>
<key>KeepAlive</key><{{KeepAlive}}/>
<key>RunAtLoad</key><{{RunAtLoad}}/>
{{if EnvVars}}<key>EnvironmentVariables</key><dict>
{{range EnvVars}}{{.}}
{{end}}</dict>{{end}}
<key>StandardOutPath</key><string>LOG_PATH</string>
<key>StandardErrorPath</key><string>LOG_PATH</string>
</dict></plist>
`

const systemdConfig = `[Unit]
Description=Driga collector

[Service]
ExecStart={{Path | cmdEscape}}{{range Arguments}} {{. | cmd}}{{end}}
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
{{range EnvVars}}{{.}}
{{end}}
[Install]
WantedBy=default.target
`
