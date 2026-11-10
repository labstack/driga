package platform

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
)

func TestProcessObserverReadsOwnIdentityAndCancellation(t *testing.T) {
	p, ok := New().(ProcessObserver)
	if !ok {
		t.Skip("identity observation unavailable on this platform")
	}
	pid := int32(os.Getpid())
	proc, present, err := p.ObserveProcess(context.Background(), pid)
	if err != nil || !present || proc.PID != pid || proc.StartTime.IsZero() || proc.Zombie {
		t.Fatalf("own identity: %+v present=%t err=%v", proc, present, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := p.ObserveProcess(ctx, pid); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled observation: %v", err)
	}
}

func TestProcessObserverConfirmsOwnedFixtureExit(t *testing.T) {
	p, ok := New().(ProcessObserver)
	if !ok {
		t.Skip("identity observation unavailable on this platform")
	}
	// Run only this test binary with no selected tests, never an agent command.
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	proc, present, err := p.ObserveProcess(context.Background(), int32(cmd.Process.Pid))
	if err != nil || present {
		t.Fatalf("reaped fixture not confirmed absent: %+v present=%t err=%v", proc, present, err)
	}
}

func TestWindowsProcessObservationDistinguishesExitAndFailure(t *testing.T) {
	const ticks = 116444736000000000 + 12345678
	for _, state := range []string{"live partial", "gone", "permission", "cancelled during query"} {
		t.Run(state, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := winPlatform{inspect: func(pid int32) (winDetails, error) {
				if pid != 42 {
					t.Fatal("observed wrong PID")
				}
				switch state {
				case "gone":
					return winDetails{}, errWinProcessGone
				case "permission":
					return winDetails{}, os.ErrPermission
				case "cancelled during query":
					cancel()
				}
				return winDetails{Start: ticks, Partial: true}, nil
			}}
			proc, present, err := p.ObserveProcess(ctx, 42)
			switch state {
			case "live partial":
				if err != nil || !present || proc.PID != 42 || !proc.StartTime.Equal(winTime(ticks)) {
					t.Fatalf("metadata failure lost live identity: %+v %t %v", proc, present, err)
				}
			case "gone":
				if err != nil || present {
					t.Fatalf("confirmed exit: present=%t err=%v", present, err)
				}
			case "permission":
				if !errors.Is(err, os.ErrPermission) {
					t.Fatalf("permission failure claimed exit: %v", err)
				}
			case "cancelled during query":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancelled query claimed exit: %v", err)
				}
			}
		})
	}
}
