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
	} {
		if got := usesAppKit(tt.args); got != tt.want {
			t.Errorf("usesAppKit(%q) = %v, want %v", tt.args, got, tt.want)
		}
	}
}
