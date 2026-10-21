//go:build !windows

package inbox

import (
	"context"
	"errors"
)

func WindowsToast(context.Context, string) error {
	return errors.New("notifications unavailable: Windows Runtime requires Windows")
}
func WindowsNotificationStatus() (string, error) {
	return "", errors.New("Windows Runtime requires Windows")
}
