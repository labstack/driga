//go:build !windows

package service

import "errors"

func newWindowsService(string) (Controller, error) {
	return nil, errors.New("Windows Task Scheduler unavailable on this OS")
}
func PrepareScheduled(string, ...string) error {
	return errors.New("--scheduled requires Windows Task Scheduler")
}
