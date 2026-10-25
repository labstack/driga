package main

import "strings"

func usesAppKit(args []string) bool {
	if command(args) != "collect" {
		return false
	}
	for _, arg := range args {
		if arg == "--notify-fd" || strings.HasPrefix(arg, "--notify-fd=") {
			return false
		}
	}
	return true
}

// appMode reports whether args run the desktop app, which must keep the
// main thread.
func appMode(args []string) bool {
	return len(args) == 1 && args[0] == "app"
}

func command(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}
