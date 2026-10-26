package main

import (
	"reflect"
	"testing"
)

func TestStartupArgs(t *testing.T) {
	const target = `{"kind":"finished","item_id":"turn-1","machine_id":"local"}`
	for _, tt := range []struct {
		name       string
		executable string
		args       []string
		app        string
		want       []string
	}{
		{"notification", "driga", []string{"app", "--notification", target}, "", []string{"app", "--notification", target}},
		{"system notification", "driga", []string{"app", "--notification", target}, "1", []string{"app", "--notification", target}},
		{"system launch", "driga", nil, "1", []string{"app"}},
		{"explicit app", "driga", []string{"app"}, "", []string{"app"}},
		{"cli", "driga", []string{"summary"}, "", []string{"summary"}},
		{"daemon", "drigad", []string{"--owner=service"}, "", []string{"service", "run", "--owner=service"}},
		{"dev daemon", "drigad-dev", []string{"--owner=service"}, "", []string{"service", "run", "--owner=service"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DRIGA_APP", tt.app)
			if got := startupArgs(tt.executable, tt.args); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("startupArgs(%q, %q) = %q; want %q", tt.executable, tt.args, got, tt.want)
			}
		})
	}
}
