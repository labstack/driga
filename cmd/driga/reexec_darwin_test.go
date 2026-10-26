//go:build darwin

package main

import (
	"errors"
	"reflect"
	"testing"
)

func TestResolvedReexecKeepsInvocationIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, real     string
		original, want []string
	}{
		{name: "daemon symlink to CLI", real: "/Applications/Driga.app/Contents/MacOS/driga", original: []string{"/opt/homebrew/bin/drigad", "--owner=service"}, want: []string{"service", "run", "--owner=service"}},
		{name: "development daemon symlink", real: "/Applications/Driga-dev.app/Contents/MacOS/driga-dev", original: []string{"/opt/homebrew/bin/drigad-dev", "--once"}, want: []string{"service", "run", "--once"}},
		{name: "CLI symlink to daemon executable", real: "/Applications/Driga.app/Contents/MacOS/drigad", original: []string{"/opt/homebrew/bin/driga", "doctor"}, want: []string{"doctor"}},
		{name: "CLI bundle identity", real: "/Applications/Driga.app/Contents/MacOS/driga", original: []string{"/opt/homebrew/bin/driga", "doctor"}, want: []string{"doctor"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			failure := errors.New("injected exec failure")
			err := reexecResolved(tc.real, tc.original, func(real string, argv, env []string) error {
				called = true
				if real != tc.real {
					t.Errorf("exec path %q, want bundle path %q", real, tc.real)
				}
				if !reflect.DeepEqual(argv, tc.original) {
					t.Errorf("exec argv %v, want original invocation %v", argv, tc.original)
				}
				if got := startupArgs(argv[0], argv[1:]); !reflect.DeepEqual(got, tc.want) {
					t.Errorf("startup after reexec: %v, want %v", got, tc.want)
				}
				return failure
			})
			if !called || !errors.Is(err, failure) {
				t.Fatalf("exec callback result: called=%t err=%v", called, err)
			}
		})
	}
}
