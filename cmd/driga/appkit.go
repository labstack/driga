package main

import "strings"

func usesAppKit(args []string) bool {
	return serviceRun(args)
}

// appMode reports whether args run the desktop app, which must keep the
// main thread.
func appMode(args []string) bool {
	return len(args) == 1 && args[0] == "app"
}

// serviceRun reports whether args run the Driga service.
func serviceRun(args []string) bool {
	var words []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			words = append(words, a)
		}
	}
	return len(words) >= 2 && words[0] == "service" && words[1] == "run"
}

func command(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}
