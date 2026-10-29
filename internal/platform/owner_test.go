package platform

import (
	"context"
	"errors"
	"os"
	"os/user"
	"runtime"
	"testing"
)

func TestProcessOwnerCollection(t *testing.T) {
	t.Run("native current process", func(t *testing.T) {
		if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
			t.Skip("native ownership addition is Linux/Windows only")
		}
		ps, e := New().Procs(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		for _, p := range ps {
			if p.PID == int32(os.Getpid()) {
				if !p.OwnerKnown {
					t.Fatal("native owner unknown")
				}
				if runtime.GOOS == "windows" {
					u, e := user.Current()
					if e != nil || p.OwnerSID != u.Uid {
						t.Fatalf("SID %s %v", p.OwnerSID, e)
					}
				} else if p.OwnerUID != uint32(os.Getuid()) {
					t.Fatal("UID mismatch")
				}
				return
			}
		}
		t.Fatal("current process missing from native collector")
	})
	t.Run("Linux UID status", func(t *testing.T) {
		for _, s := range []string{"Name:\tfixture\nUid:\t501\t501\t501\t501\n", "Uid: 501 502 503 504\n"} {
			uid, ok := procOwnerUID([]byte(s))
			if !ok || uid != 501 {
				t.Fatalf("%d %t", uid, ok)
			}
		}
		for _, s := range []string{"", "Uid: nope 501", "Uid: -1", "Uid: 4294967296"} {
			if _, ok := procOwnerUID([]byte(s)); ok {
				t.Fatalf("accepted %q", s)
			}
		}
	})
	t.Run("Windows SID", func(t *testing.T) {
		p := winPlatform{snapshot: func() ([]winEntry, error) { return []winEntry{{PID: 400001}, {PID: 400002}}, nil }, inspect: func(pid int32) (winDetails, error) {
			if pid == 400002 {
				return winDetails{}, errors.New("owner query denied")
			}
			return winDetails{OwnerSID: "S-1-5-21-fixture", OwnerKnown: true}, nil
		}}
		ps, e := p.Procs(context.Background())
		if e != nil || len(ps) != 2 || !ps[0].OwnerKnown || ps[0].OwnerSID != "S-1-5-21-fixture" || ps[1].OwnerKnown || ps[1].OwnerSID != "" {
			t.Fatalf("%#v %v", ps, e)
		}
	})
}
