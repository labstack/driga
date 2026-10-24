//go:build !darwin

package attention

import "fmt"

func MacNotificationStatus() (string, error) {
	return "", fmt.Errorf("native macOS notifications require Driga.app on macOS")
}

func MacDesktopNotifier() Notifier { return &MacNotifier{} }

func DesktopNotificationsReady() bool { return true }

func RequestNotificationPermission() {}
