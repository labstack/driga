package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/labstack/driga/internal/config"
)

// RemoveLegacy unloads and deletes the launch agent releases before drigad
// installed, so the collector it ran does not compete with the service.
func RemoveLegacy() error {
	// Only a release installed the collector; a development copy never did.
	if Variant() != "" {
		return nil
	}
	plist := filepath.Join(config.ExpandHome("~"), "Library", "LaunchAgents", legacyLabel+".plist")
	if _, err := os.Stat(plist); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	// It may already be unloaded; removing the file is what matters.
	_ = exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), legacyLabel)).Run()
	return os.Remove(plist)
}
