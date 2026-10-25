package main

import "testing"

func TestUsesAppKit(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"collect"}, true},
		{[]string{"collect", "--once"}, true},
		{[]string{"collect", "--notify-fd", "3"}, false},
		{[]string{"collect", "--notify-fd=3"}, false},
		{[]string{"collect", "--notify-fd=0"}, false},
		{[]string{"--no-color", "collect", "--notify-fd", "3"}, false},
		{[]string{"collect", "--cpu-profile=notify-fd"}, true},
		{[]string{"top"}, false},
		{[]string{"doctor"}, false},
		{[]string{"app"}, false},
	} {
		if got := usesAppKit(tt.args); got != tt.want {
			t.Errorf("usesAppKit(%q) = %v, want %v", tt.args, got, tt.want)
		}
	}
}

// The desktop app keeps the main thread: its toolkit must run there.
func TestAppMode(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want bool
	}{
		{[]string{"app"}, true},
		{nil, false},
		{[]string{"app", "--help"}, false},
		{[]string{"collect"}, false},
		{[]string{"top"}, false},
	} {
		if got := appMode(tt.args); got != tt.want {
			t.Errorf("appMode(%q) = %v, want %v", tt.args, got, tt.want)
		}
	}
}
