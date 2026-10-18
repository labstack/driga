// Package service configures the optional per-user launchd collector.
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
func LogPath() string {
	return filepath.Join(config.ExpandHome("~"), "Library", "Logs", "driga", "collector.log")
}

// Config returns configuration only; it never contacts the service manager.
func Config(binPath string) *ks.Config {
	// Darwin's LogDirectory option fixes the directory but not the filenames.
	// LaunchdConfig supports directing both streams to the single documented file.
	escape := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;", "{", "&#123;")
	template := strings.ReplaceAll(launchdConfig, "LOG_PATH", escape.Replace(LogPath()))
	env := map[string]string{"HOME": config.ExpandHome("~")}
	for _, key := range []string{"DRIGA_CONFIG", "DRIGA_DATA_DIR", "DRIGA_SPOOL_DIR", "CODEX_HOME"} {
		if value := os.Getenv(key); value != "" {
			env[key] = value
		}
	}
	return &ks.Config{
		Name: "run.driga.collector", DisplayName: "driga collector",
		Executable: binPath, Arguments: []string{"collect"}, EnvVars: env,
		Option: ks.KeyValue{"UserService": true, "KeepAlive": true, "RunAtLoad": true,
			"LogDirectory": filepath.Dir(LogPath()), "LaunchdConfig": template},
	}
}

// New creates a control handle; installation and startup remain explicit actions.
func New(binPath string) (ks.Service, error) {
	if runtime.GOOS != "darwin" {
		return nil, errors.New("driga service requires macOS launchd")
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
