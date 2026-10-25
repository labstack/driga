//go:build !darwin && !windows

package service

import (
	"errors"

	ks "github.com/kardianos/service"
)

// RemoveLegacy stops and deletes the systemd unit releases before drigad
// installed, so the collector it ran does not compete with the service.
func RemoveLegacy() error {
	// Only a release installed the collector; a development copy never did.
	if Variant() != "" {
		return nil
	}
	c := Config("/driga")
	c.Name = legacyLabel
	s, err := ks.New(controlOnly{}, c)
	if err != nil {
		return err
	}
	if _, err := s.Status(); errors.Is(err, ks.ErrNotInstalled) {
		return nil
	}
	_ = s.Stop()
	return s.Uninstall()
}
