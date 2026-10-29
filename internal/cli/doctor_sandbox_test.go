package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/command"
)

func TestDoctorSandbox(t *testing.T) {
	for _, tc := range []struct {
		name, goos          string
		cap                 command.Capability
		status, remediation string
	}{
		{"Seatbelt", "darwin", command.Capability{Available: true, Backend: "/usr/bin/sandbox-exec", OSVersion: "15.7"}, "ok", ""},
		{"Seatbelt probe failure", "darwin", command.Capability{Backend: "/usr/bin/sandbox-exec", OSVersion: "15.7", Reason: "sandbox initialization failed"}, "warn", "sandbox-exec"},
		{"bubblewrap", "linux", command.Capability{Available: true, Backend: "/usr/bin/bwrap", OSVersion: "6.8.0"}, "ok", ""},
		{"missing bwrap", "linux", command.Capability{Reason: "Running commands needs bubblewrap (bwrap), which is missing."}, "warn", "install bubblewrap"},
		{"unusable bwrap", "linux", command.Capability{Backend: "/usr/bin/bwrap", Reason: "creating new namespace: Operation not permitted"}, "warn", "AppArmor"},
		{"Windows", "windows", command.Capability{Reason: "Running commands needs a sandbox, which Windows does not have yet."}, "info", "Read and Process"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _ := doctorFixture(t)
			d.OS = tc.goos
			d.NotificationStatus = func() (string, error) { return "fixture", nil }
			calls := 0
			d.SandboxProbe = func(ctx context.Context) command.Capability {
				if ctx.Err() != nil {
					t.Fatal(ctx.Err())
				}
				calls++
				return tc.cap
			}
			c := requireCheck(t, Doctor(context.Background(), d), "chat_sandbox", tc.status, "")
			if calls != 1 || !strings.Contains(c.Detail, tc.cap.Reason) || !strings.Contains(c.Detail, tc.cap.Backend) || !strings.Contains(c.Detail, tc.cap.OSVersion) || !strings.Contains(c.Fix, tc.remediation) {
				t.Fatalf("calls=%d check=%+v", calls, c)
			}
			if tc.goos == "windows" && c.Detail != tc.cap.Reason {
				t.Fatal(c.Detail)
			}
		})
	}
}
