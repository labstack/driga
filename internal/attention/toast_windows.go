package attention

import (
	"context"
	"errors"
	"fmt"
	"unsafe"

	"github.com/labstack/driga/internal/wincom"
	"golang.org/x/sys/windows/registry"
)

const toastAppID = "LabStack.Driga"
const toastRegistryPath = `Software\Classes\AppUserModelId\` + toastAppID

// Register only a per-user notification identity. Plain toasts have no COM
// activation server, protocol handler, shell command or callback into Go.
func registerToastIdentity() error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, toastRegistryPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	name, _, err := key.GetStringValue("DisplayName")
	if err == nil && name != "Driga" {
		return errors.New("Driga notification identity is already in use")
	}
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return key.SetStringValue("DisplayName", "Driga")
}

// WindowsToast sends through the Windows Runtime ABI; it never starts a shell.
// All interfaces and HSTRINGs are released on the initialized OS thread.
func WindowsToast(ctx context.Context, payload string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := registerToastIdentity(); err != nil {
		return fmt.Errorf("notifications unavailable: %w", err)
	}
	done, err := wincom.Initialize()
	if err != nil {
		return fmt.Errorf("notifications unavailable: %w", err)
	}
	defer done()
	notifier, err := toastNotifier()
	if err != nil {
		return fmt.Errorf("notifications unavailable: %w", err)
	}
	defer notifier.Release()
	var setting uint32
	if err := notifier.Call(8, uintptr(unsafe.Pointer(&setting))); err != nil {
		return err
	}
	if setting != 0 {
		return fmt.Errorf("notifications unavailable: Windows notification setting %d (disabled by user, policy, or manifest)", setting)
	}
	doc, err := wincom.Activate("Windows.Data.Xml.Dom.XmlDocument")
	if err != nil {
		return err
	}
	defer doc.Release()
	io, err := doc.Query("{6CD0E74E-EE65-4489-9EBF-CA43E87BA637}")
	if err != nil {
		return err
	}
	defer io.Release()
	text, err := wincom.String(payload)
	if err != nil {
		return err
	}
	defer text.Close()
	if err := io.Call(6, uintptr(text)); err != nil {
		return err
	}
	xml, err := doc.Query("{F7F3A506-1E87-42D6-BCFB-B8C809FA5494}")
	if err != nil {
		return err
	}
	defer xml.Release()
	factory, err := wincom.Factory("Windows.UI.Notifications.ToastNotification", "{04124B20-82C6-4229-B109-FD9ED4662B53}")
	if err != nil {
		return err
	}
	defer factory.Release()
	var toast *wincom.Object
	if err := factory.Call(6, uintptr(unsafe.Pointer(xml)), uintptr(unsafe.Pointer(&toast))); err != nil {
		return err
	}
	defer toast.Release()
	if err := ctx.Err(); err != nil {
		return err
	}
	return notifier.Call(6, uintptr(unsafe.Pointer(toast)))
}

func toastNotifier() (*wincom.Object, error) {
	manager, err := wincom.Factory("Windows.UI.Notifications.ToastNotificationManager", "{50AC103F-D235-4598-BBEF-98FE4D1A3AD4}")
	if err != nil {
		return nil, err
	}
	defer manager.Release()
	id, err := wincom.String(toastAppID)
	if err != nil {
		return nil, err
	}
	defer id.Close()
	var notifier *wincom.Object
	err = manager.Call(7, uintptr(id), uintptr(unsafe.Pointer(&notifier)))
	return notifier, err
}

// WindowsNotificationStatus is read-only: doctor never registers an identity or
// displays a notification. Notify creates the identity on the first alert.
func WindowsNotificationStatus() (string, error) {
	done, err := wincom.Initialize()
	if err != nil {
		return "", err
	}
	defer done()
	manager, err := wincom.Factory("Windows.UI.Notifications.ToastNotificationManager", "{50AC103F-D235-4598-BBEF-98FE4D1A3AD4}")
	if err != nil {
		return "", err
	}
	manager.Release()
	key, err := registry.OpenKey(registry.CURRENT_USER, toastRegistryPath, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return "native WinRT plain toast; identity registered on first alert", nil
	}
	if err != nil {
		return "", err
	}
	defer key.Close()
	name, _, err := key.GetStringValue("DisplayName")
	if err != nil {
		return "", err
	}
	if name != "Driga" {
		return "", errors.New("notification identity is already in use")
	}
	notifier, err := toastNotifier()
	if err != nil {
		return "", err
	}
	defer notifier.Release()
	var setting uint32
	if err := notifier.Call(8, uintptr(unsafe.Pointer(&setting))); err != nil {
		return "", err
	}
	if setting != 0 {
		return "", fmt.Errorf("Windows notification setting %d (disabled by user, policy, or manifest)", setting)
	}
	return "native WinRT plain toast (delivery depends on Windows notification settings)", nil
}
