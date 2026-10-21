package derive

import (
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestLaunchSurface(t *testing.T) {
	start := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, exe, want string
		parentStart     time.Time
	}{
		{"terminal", "/Applications/Fixture Terminal.app/Contents/MacOS/terminal", "Fixture Terminal", start.Add(-time.Hour)},
		{"editor", "/Applications/Fixture Editor.app/Contents/MacOS/editor", "Fixture Editor", start.Add(-time.Hour)},
		{"nearest bundle", "/Applications/Fixture Editor.app/Contents/Frameworks/Helper.app/Contents/MacOS/helper", "Helper", start.Add(-time.Hour)},
		{"no bundle", "/usr/bin/sh", "", start.Add(-time.Hour)},
		{"reused parent", "/Applications/Unrelated.app/Contents/MacOS/app", "", start.Add(time.Second)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ps := []model.Proc{{PID: 30, PPID: 20, StartTime: start}, {PID: 20, PPID: 10, StartTime: start}, {PID: 10, Exe: tc.exe, StartTime: tc.parentStart}}
			byPID := map[int32]model.Proc{}
			for _, p := range ps {
				byPID[p.PID] = p
			}
			if got := LaunchSurface(byPID, 30); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			wantBundle := ""
			if tc.want != "" {
				wantBundle = AppBundle(tc.exe)
			}
			if got := PlaceProcesses(ps, nil, nil)[30].Bundle; got != wantBundle {
				t.Fatalf("placement bundle %q, want %q", got, wantBundle)
			}
		})
	}
	if got := LaunchSurface(nil, 99); got != "" {
		t.Fatal(got)
	}
	if got := LaunchSurface(map[int32]model.Proc{1: {PID: 1, PPID: 2}, 2: {PID: 2, PPID: 1}}, 1); got != "" {
		t.Fatal(got)
	}
	if got := LaunchSurface(map[int32]model.Proc{1: {PID: 1, Zombie: true, Exe: "/A.app/Contents/MacOS/A"}}, 1); got != "" {
		t.Fatal(got)
	}
}
