//go:build !windows

package attention

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
