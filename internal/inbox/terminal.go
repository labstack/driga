package inbox

import (
	"context"
	"strings"
	"time"

	"github.com/labstack/driga/internal/model"
)

const terminalProofScript = `on run argv
set wanted to item 1 of argv
if application "Terminal" is running then
 tell application "Terminal"
  repeat with w in windows
   repeat with t in tabs of w
    if tty of t is wanted then return "Terminal"
   end repeat
  end repeat
 end tell
end if
if application "iTerm2" is running then
 tell application "iTerm2"
  repeat with w in windows
   repeat with t in tabs of w
    repeat with s in sessions of t
     if tty of s is wanted then return "iTerm2"
    end repeat
   end repeat
  end repeat
 end tell
end if
end run`

// TerminalJump is called only when the operator has opted into app scripting.
func TerminalJump(ctx context.Context, r Runner, tty string) *model.InboxJump {
	if r == nil || !strings.HasPrefix(tty, "/dev/") {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	b, err := r.Run(ctx, "osascript", "-e", terminalProofScript, tty)
	if err != nil {
		return nil
	}
	app := strings.TrimSpace(string(b))
	if app != "Terminal" && app != "iTerm2" {
		return nil
	}
	script := TerminalActivateScript(app)
	return &model.InboxJump{Kind: "terminal", Target: tty, App: app, Command: "osascript -e " + Quote(script) + " " + Quote(tty)}
}

// TerminalActivateScript rechecks the exact TTY before selecting a window.
func TerminalActivateScript(app string) string {
	if app == "Terminal" {
		return `on run argv
tell application "Terminal"
 repeat with w in windows
  repeat with t in tabs of w
   if tty of t is item 1 of argv then
    set selected tab of w to t
    set index of w to 1
    activate
    return
   end if
  end repeat
 end repeat
end tell
end run`
	}
	if app == "iTerm2" {
		return `on run argv
tell application "iTerm2"
 repeat with w in windows
  repeat with t in tabs of w
   repeat with s in sessions of t
    if tty of s is item 1 of argv then
     select s
     select w
     activate
     return
    end if
   end repeat
  end repeat
 end repeat
end tell
end run`
	}
	return ""
}
