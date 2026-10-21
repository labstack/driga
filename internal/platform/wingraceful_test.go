package platform

import "testing"

func TestWindowsGracefulPlan(t *testing.T) {
	for _, tc := range []struct {
		group, console, closed bool
		want                   gracefulStep
	}{
		{false, true, true, closeWindows}, {true, false, true, closeWindows},
		{true, true, false, closeWindows}, {true, true, true, consoleBreak},
	} {
		if got := planWindowsGraceful(tc.group, tc.console, tc.closed); got != tc.want {
			t.Fatalf("%+v: %v", tc, got)
		}
	}
}
