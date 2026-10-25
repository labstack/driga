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

func command(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}
