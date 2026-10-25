package desktop

import (
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"sync"
)

// page is the part of the main window the app drives.
type page interface {
	// Show brings the window to the front.
	Show()
	// Eval runs a script in the page without waiting for it.
	Eval(js string)
}

// windowHost owns the one main window. It creates the window on demand and
// holds page commands until its first page has loaded, so a menu item or a
// notification clicked while no window is open still reaches the page. Its
// methods are called on the main thread, as the toolkit calls its listeners.
// create runs outside its lock: the toolkit can call those listeners, and so
// the host, while it opens a window.
type windowHost struct {
	// create opens a new window and calls loaded each time its page finishes
	// loading.
	create func(loaded func()) page

	mu       sync.Mutex
	win      page
	gen      int  // Advances with every window, so a closed one's loads are ignored.
	creating bool // A window is being created; win is set once it is.
	closed   []page
	early    bool // The window being created loaded already.
	ready    bool
	pending  []string
}

// Open shows the window, creating it when none is open.
func (h *windowHost) Open() { h.run("") }

// IsOpen reports whether the main window exists.
func (h *windowHost) IsOpen() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.win != nil
}

// Current is the open main window, or nil.
func (h *windowHost) Current() page {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.win
}

// Command shows the window and calls the page's drigaCommand(name, arg).
func (h *windowHost) Command(name string, arg any) {
	n, _ := json.Marshal(name)
	a, _ := json.Marshal(arg)
	h.run("window.drigaCommand(" + string(n) + "," + string(a) + ")")
}

// Closed forgets the window once it has closed.
func (h *windowHost) Closed(p page) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case h.win == p:
		h.win, h.ready, h.pending = nil, false, nil
	case h.creating:
		// Perhaps the window being created, not known until create returns.
		h.closed = append(h.closed, p)
	}
}

func (h *windowHost) run(js string) {
	h.mu.Lock()
	win, ready := h.win, h.ready
	if win == nil {
		if js != "" {
			h.pending = append(h.pending, js)
		}
		if h.creating {
			// The window being created runs js once its page loads.
			h.mu.Unlock()
			return
		}
		h.creating, h.early, h.closed = true, false, nil
		h.gen++
		gen := h.gen
		h.mu.Unlock()
		win = h.create(func() { h.loaded(gen) })
		h.mu.Lock()
		closed := slices.Contains(h.closed, win)
		early := h.early
		h.creating, h.early, h.closed = false, false, nil
		if closed {
			h.pending = nil
			h.mu.Unlock()
			return
		}
		h.win, h.ready = win, false
		h.mu.Unlock()
		if early {
			h.loaded(gen)
		}
		return
	}
	defer win.Show()
	if js != "" && !ready {
		h.pending = append(h.pending, js)
		js = ""
	}
	h.mu.Unlock()
	if js != "" {
		win.Eval(js)
	}
}

// loaded runs the commands that waited for the page, once per window.
func (h *windowHost) loaded(gen int) {
	h.mu.Lock()
	if gen == h.gen && h.creating {
		h.early = true
		h.mu.Unlock()
		return
	}
	if gen != h.gen || h.win == nil || h.ready {
		h.mu.Unlock()
		return
	}
	win, pending := h.win, h.pending
	h.ready, h.pending = true, nil
	h.mu.Unlock()
	for _, js := range pending {
		win.Eval(js)
	}
}

// CloseQuits reports whether closing the window quits Driga. It keeps
// collecting while an icon is left to bring it back: the Dock or menu bar on
// macOS, a tray icon elsewhere.
func CloseQuits(goos string, trayCreated bool) bool {
	return goos != "darwin" && !trayCreated
}

// dockRegular reports whether the macOS Dock shows Driga: while a window is
// open, and always when there is no menu bar icon to open it from.
func dockRegular(windowOpen, trayCreated bool) bool {
	return windowOpen || !trayCreated
}

// startHidden reports whether a launch opens no window: a login launch, where
// an icon is left to open it from.
func startHidden(goos string, openedAtLogin, trayCreated bool) bool {
	return openedAtLogin && !CloseQuits(goos, trayCreated)
}

// confirmDialog turns a page's confirm() message into a native dialog: the
// question, the sentences after it as detail, and buttons naming the action.
func confirmDialog(message string) (text, detail string, buttons []string) {
	text = message
	if i := strings.Index(message, "? "); i >= 0 && i+2 < len(message) && !strings.HasSuffix(message, "?") {
		text, detail = message[:i+1], strings.TrimSpace(message[i+2:])
	}
	if strings.HasPrefix(message, "Stop ") {
		return text, detail, []string{"Stop", "Cancel"}
	}
	return text, detail, []string{"OK", "Cancel"}
}

// external reports whether a link leaves the app for the browser: any web
// page but the app's own, which WebView2 serves at http://mygo.localhost.
func external(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return u.Hostname() != "mygo.localhost"
}
