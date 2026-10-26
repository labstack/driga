package attention

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/labstack/driga/internal/model"
	"log/slog"
	"path/filepath"
	"sync"
	"time"
)

// ErrNotificationsDenied means macOS has Driga's notifications turned off.
var ErrNotificationsDenied = errors.New("Driga is not allowed to send notifications")

// MacNotificationSettings opens Driga's page in System Settings > Notifications.
// macOS asks only once, so after a refusal or a prompt withdrawn by a restart
// this is the only way to turn notifications on.
const MacNotificationSettings = `open "x-apple.systempreferences:com.apple.Notifications-Settings.extension?id=run.driga"`

// NativeNotificationRequest is the value-only boundary to UserNotifications.
// No Objective-C objects escape the Darwin implementation.
type NativeNotificationRequest struct {
	Identifier, ThreadIdentifier string
	Title, Body, Sound           string
	ItemID, MachineID, Kind      string
}

type NativeNotificationCenter interface {
	Authorize(func(error))
	Add(context.Context, NativeNotificationRequest) error
}

// MacNotifier asks macOS for permission before posting. After a refusal it asks
// again on the next notification; macOS answers from the saved setting without
// prompting, so allowing Driga in System Settings takes effect without a restart.
// Authorization can outlive a cancelled Notify call.
type MacNotifier struct {
	Center     NativeNotificationCenter
	mu         sync.Mutex
	authorized chan struct{}
	authErr    error
	pending    map[string]Notification // queued while permission is undecided
}

func (m *MacNotifier) Notify(ctx context.Context, n Notification) error {
	if m.Center == nil {
		return fmt.Errorf("notifications unavailable: install Driga.app for native macOS notifications")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ch := m.authorize()
	select {
	case <-ch:
		return m.post(ctx, n)
	default:
	}
	// While the user considers permission, keep only the latest alert per
	// destination and post them once answered; collection never waits.
	m.mu.Lock()
	if m.pending == nil {
		m.pending = map[string]Notification{}
		go m.flush(ctx, ch)
	}
	m.pending[nativeNotificationRequest(n).Identifier] = n
	m.mu.Unlock()
	return nil
}

// RequestPermission asks macOS once at startup, while the user who just
// installed or restarted the service is likely present. macOS shows its prompt
// only while undecided and answers silently from the saved setting otherwise.
func (m *MacNotifier) RequestPermission() {
	if m.Center != nil {
		m.authorize()
	}
}

// authorize starts at most one permission request and returns the channel
// closed when macOS answers.
func (m *MacNotifier) authorize() chan struct{} {
	m.mu.Lock()
	ch, start := m.authorized, m.authorized == nil
	if start {
		ch = make(chan struct{})
		m.authorized = ch
	}
	m.mu.Unlock()
	if start { // outside the lock: the callback may run synchronously
		m.Center.Authorize(func(err error) {
			if err != nil {
				slog.Warn("macOS notification permission refused", "event", "notifications_refused", "err", err.Error())
			}
			m.mu.Lock()
			m.authErr = err
			if err != nil {
				m.authorized = nil // re-ask on the next notification
			}
			m.mu.Unlock()
			close(ch)
		})
	}
	return ch
}

func (m *MacNotifier) flush(ctx context.Context, ch chan struct{}) {
	select {
	case <-ctx.Done():
	case <-ch:
	}
	m.mu.Lock()
	queued := m.pending
	m.pending = nil
	m.mu.Unlock()
	for _, n := range queued {
		_ = m.post(ctx, n)
	}
}

func (m *MacNotifier) err() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.authErr
}

func (m *MacNotifier) post(ctx context.Context, n Notification) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := m.Center.Add(ctx, nativeNotificationRequest(n)); err != nil {
		slog.Warn("macOS notification not delivered", "event", "notification_failed", "err", err.Error())
		return err
	}
	return nil
}

func nativeNotificationRequest(n Notification) NativeNotificationRequest {
	key := "item:" + n.ItemID
	if n.ItemID == "" {
		key = "summary:" + n.Kind
	}
	// Machine identity prevents local/remote ID collisions; hashing also avoids
	// ambiguous delimiters and puts no private item text in the identifier.
	id := fmt.Sprintf("driga-%x", sha256.Sum256([]byte(n.MachineID+"\x00"+key)))
	return NativeNotificationRequest{Identifier: id, ThreadIdentifier: id,
		Title: n.Title, Body: n.Body, Sound: n.Sound, ItemID: n.ItemID, MachineID: n.MachineID, Kind: n.Kind}
}

const notificationDefaultAction = "com.apple.UNNotificationDefaultActionIdentifier"

// notificationResponse sends a value-only target on stdin; identifiers never
// enter argv. The app resolves it against its current store.
func notificationResponse(ctx context.Context, r Runner, path, action, itemID, machineID, kind string) error {
	if action != notificationDefaultAction || path == "" || r == nil {
		return nil
	}
	if filepath.Base(path) == "drigad" {
		path = filepath.Join(filepath.Dir(path), "driga")
	}
	target, _ := json.Marshal(model.NotificationTarget{ItemID: itemID, MachineID: machineID, Kind: kind})
	input, ok := r.(interface {
		RunInput(context.Context, []byte, string, ...string) ([]byte, error)
	})
	if !ok {
		return errors.New("notification runner has no private input transport")
	}
	_, err := input.RunInput(ctx, target, path, "app", "--notification-stdin")
	return err
}

// drigaAppExecutable reports whether path is one of Driga.app's executables:
// driga, the app and CLI, or drigad, the Driga service.
func drigaAppExecutable(path string) bool {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	base := filepath.Base(resolved)
	return (base == "driga" || base == "drigad") && filepath.Base(filepath.Dir(resolved)) == "MacOS" &&
		filepath.Base(filepath.Dir(filepath.Dir(resolved))) == "Contents" &&
		filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(resolved)))) == "Driga.app"
}
