//go:build !darwin

package service

import "errors"

func newLaunchdController(string) (Controller, error) {
	return nil, errors.New("launchd requires macOS")
}
