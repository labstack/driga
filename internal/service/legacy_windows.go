package service

import (
	"errors"
	"strings"

	ks "github.com/kardianos/service"
)

// RemoveLegacy stops and deletes the scheduled task releases before drigad
// installed, so the collector it ran does not compete with the service.
func RemoveLegacy() error {
	// Only a release installed the collector; a development copy never did.
	if Variant() != "" {
		return nil
	}
	c, err := newWindowsService(`C:\driga.exe`)
	if err != nil {
		return err
	}
	t := c.(*taskService)
	t.name = strings.TrimSuffix(t.name, `\Driga service`+variantName()) + `\Driga collector`
	if _, err := t.TaskState(); errors.Is(err, ks.ErrNotInstalled) {
		return nil
	}
	return t.Uninstall()
}
