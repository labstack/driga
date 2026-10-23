package cli

import (
	"context"

	"golang.org/x/sys/windows"
)

var shellOpenBrowser = windows.ShellExecute

func openWebBrowser(ctx context.Context, address string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	url, err := windows.UTF16PtrFromString(address)
	if err != nil {
		return err
	}
	return shellOpenBrowser(0, verb, url, nil, nil, windows.SW_SHOWNORMAL)
}
