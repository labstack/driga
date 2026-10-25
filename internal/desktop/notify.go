package desktop

import (
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"sync"

	"github.com/egoist/mygo"
	"github.com/labstack/driga/internal/attention"
	"github.com/labstack/driga/internal/view"
)

const notificationPrefix = "driga:"

// notificationID names a notification by what it is about, so a newer one
// about the same item replaces it, and a click, even one that launches the
// app after it quit, finds the item again.
func notificationID(n attention.Notification) string {
	v := url.Values{}
	if n.ItemID != "" {
		v.Set("item", n.ItemID)
		v.Set("machine", n.MachineID)
	} else {
		v.Set("summary", n.Kind)
	}
	return notificationPrefix + v.Encode()
}

// parseNotificationID reads back the item a notification is about.
func parseNotificationID(id string) (attention.Notification, bool) {
	raw, ok := strings.CutPrefix(id, notificationPrefix)
	if !ok {
		return attention.Notification{}, false
	}
	v, err := url.ParseQuery(raw)
	if err != nil {
		return attention.Notification{}, false
	}
	return attention.Notification{ItemID: v.Get("item"), MachineID: v.Get("machine"), Kind: v.Get("summary")}, true
}

// ClickTarget is the session a notification is about, found among the inbox
// rows by its item and machine. Summaries and machine health have none.
func ClickTarget(n attention.Notification, rows []view.InboxRow) (sessionID int64, ok bool) {
	if n.ItemID == "" {
		return 0, false
	}
	for _, row := range rows {
		if row.ID == n.ItemID && row.MachineID == n.MachineID && row.SessionID > 0 {
			return row.SessionID, true
		}
	}
	return 0, false
}

// openNotification answers a click: the window comes forward, created if
// needed, on the session the notification is about.
func openNotification(h *windowHost, id string, rows func() []view.InboxRow) {
	n, ok := parseNotificationID(id)
	if !ok {
		return
	}
	if session, ok := ClickTarget(n, rows()); ok {
		h.Command("openSession", session)
		return
	}
	h.Open()
}

// notifier posts the collector's notifications as the app's own.
type notifier struct {
	mu     sync.Mutex
	denied bool
}

func (p *notifier) post(n attention.Notification) {
	note := mygo.NewNotification(mygo.NotificationOptions{
		ID: notificationID(n), Title: n.Title, Body: n.Body, Silent: n.Sound == "",
	})
	err := note.Show()
	if err == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if errors.Is(err, mygo.ErrNotificationsDenied) {
		if !p.denied {
			slog.Warn("notifications are turned off for Driga", "event", "notifications_refused")
		}
		p.denied = true
		return
	}
	slog.Warn("notification not delivered", "event", "notification_failed", "err", err.Error())
}
