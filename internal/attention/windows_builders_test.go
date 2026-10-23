package attention

import (
	"context"
	"encoding/xml"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/model"
)

func TestWindowsTerminalCommand(t *testing.T) {
	if got := windowsTerminalLine(`C:\work&界`); got != `cmd.exe /c start "" /D "C:\work&界" cmd.exe` {
		t.Fatal(got)
	}
	for _, tc := range []struct {
		wt   string
		want []string
	}{
		{`C:\WindowsApps\wt.exe`, []string{`C:\WindowsApps\wt.exe`, "-d", `C:\work & 界`}},
		{"", []string{"cmd.exe", "/c", "start", "", "/D", `C:\work & 界`, "cmd.exe"}},
	} {
		got, err := windowsTerminalCommand(tc.wt, `C:\work & 界`)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatal(got, err)
		}
	}
	for _, ok := range []string{`C:\Program Files (x86)\app`, `C:\R&D\work`, `C:\a^b`} {
		if _, err := windowsTerminalCommand("", ok); err != nil {
			t.Fatal("refused", ok, err)
		}
	}
	for _, bad := range []string{"relative", `C:relative`, "C:\\a\n", `C:\%PATH%`, `C:\a;new-tab calc`, `C:\a;b`} {
		if _, err := windowsTerminalCommand("", bad); err == nil {
			t.Fatal("accepted", bad)
		}
	}
}

func TestWindowsSessionJumpOnlyOpensWorkingDirectory(t *testing.T) {
	var calls [][]string
	r := runnerFunc(func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		return nil, nil
	})
	j := &model.InboxJump{Kind: "resume", Cwd: `C:\work & 界`, Command: `never execute this & del *`}
	opts := JumpOptions{OS: "windows", TerminalLookup: func(name string) (string, error) {
		if name != "wt.exe" {
			t.Fatal(name)
		}
		return `.\wt.exe`, exec.ErrDot // A lookup error must never execute this path.
	}}
	if _, err := RunJump(context.Background(), r, j, opts); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, [][]string{{"cmd.exe", "/c", "start", "", "/D", j.Cwd, "cmd.exe"}}) {
		t.Fatal(calls)
	}
	j.Kind = "tmux"
	if _, err := RunJump(context.Background(), r, j, opts); err == nil {
		t.Fatal("accepted a Unix terminal destination")
	}
}

func TestToastPayload(t *testing.T) {
	got, err := toastPayload(Notification{Title: `<Needs & "you">`, Body: "界 & <body>", Sound: "Glass", ItemID: `ignored <item>`})
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Visual struct {
			Binding struct {
				Template string   `xml:"template,attr"`
				Text     []string `xml:"text"`
			} `xml:"binding"`
		} `xml:"visual"`
		Audio struct {
			Src string `xml:"src,attr"`
		} `xml:"audio"`
	}
	if err := xml.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parsed.Visual.Binding.Text, []string{`<Needs & "you">`, "界 & <body>"}) || parsed.Visual.Binding.Template != "ToastGeneric" || !strings.HasPrefix(parsed.Audio.Src, "ms-winsoundevent:") {
		t.Fatal(got)
	}
	if strings.Contains(got, "ignored") || strings.Contains(got, "launch=") {
		t.Fatal("plain toast must not execute input", got)
	}
	if _, err := toastPayload(Notification{Body: "bad\x00text"}); err == nil {
		t.Fatal("NUL accepted")
	}
}
