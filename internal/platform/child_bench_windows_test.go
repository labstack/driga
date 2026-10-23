package platform

import (
	"os/exec"
	"testing"
)

// Keep job ownership overhead visible on machines with many system threads.
func BenchmarkWindowsChildStart(b *testing.B) {
	for _, managed := range []bool{false, true} {
		name := "exec"
		if managed {
			name = "job"
		}
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				cmd := exec.Command("cmd.exe", "/d", "/c", "exit", "0")
				var err error
				if managed {
					err = RunChild(cmd)
				} else {
					err = cmd.Run()
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
