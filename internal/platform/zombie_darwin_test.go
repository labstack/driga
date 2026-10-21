package platform

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestDarwinKinfoProcZombie(t *testing.T) {
	// XNU's sys/proc.h defines SZOMB as 5. No live kernel reads.
	for _, state := range []int8{0, 1, 2, 3, 4, 5, 6, 7} {
		var k unix.KinfoProc
		k.Proc.P_pid = 9441
		k.Proc.P_stat = state
		k.Eproc.Ppid = 9000
		p := procFromKinfo(&k)
		if p.PID != 9441 || p.PPID != 9000 || p.Zombie != (state == 5) {
			t.Errorf("p_stat %d: %+v", state, p)
		}
	}
}
