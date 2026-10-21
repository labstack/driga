package inbox

import (
	"encoding/xml"
	"fmt"
	"os/exec"
	"strings"

	"github.com/labstack/driga/internal/pathx"
)

func windowsTerminalCommand(wt, cwd string) ([]string, error) {
	if !pathx.WindowsStyle(cwd) || !pathx.RecordedIsAbs(cwd) || strings.ContainsAny(cwd, "\x00\r\n\"%!") {
		return nil, fmt.Errorf("terminal requires an absolute Windows directory without expansion characters")
	}
	if wt != "" {
		return []string{wt, "-d", cwd}, nil
	}
	return []string{"cmd.exe", "/c", "start", "", "/D", cwd, "cmd.exe"}, nil
}

// Used only after windowsTerminalCommand has validated the directory.
func windowsTerminalLine(cwd string) string { return `cmd.exe /c start "" /D "` + cwd + `" cmd.exe` }

// WindowsTerminalCommand selects Windows Terminal, then the built-in shell.
func WindowsTerminalCommand(cwd string) ([]string, error) {
	wt, err := exec.LookPath("wt.exe")
	if err != nil {
		wt = ""
	}
	return windowsTerminalCommand(wt, cwd)
}

// Plain toasts deliberately carry no executable or protocol activation data.
func toastPayload(n Notification) (string, error) {
	if strings.ContainsRune(n.Title+n.Body, 0) {
		return "", fmt.Errorf("toast text contains NUL")
	}
	type binding struct {
		Template string   `xml:"template,attr"`
		Text     []string `xml:"text"`
	}
	type audio struct {
		Src string `xml:"src,attr"`
	}
	payload := struct {
		XMLName xml.Name `xml:"toast"`
		Binding binding  `xml:"visual>binding"`
		Audio   audio    `xml:"audio"`
	}{Binding: binding{"ToastGeneric", []string{n.Title, n.Body}}, Audio: audio{"ms-winsoundevent:Notification.Default"}}
	if n.Kind == "finished" {
		payload.Audio.Src = "ms-winsoundevent:Notification.IM"
	}
	b, err := xml.Marshal(payload)
	return string(b), err
}
