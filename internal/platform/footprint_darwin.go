package platform

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// proc_pid_rusage's documented libproc wrapper invokes __proc_info with
// PROC_INFO_CALL_PIDRUSAGE=9, flavor RUSAGE_INFO_V2=2, buffer size argument 0.
// https://github.com/apple-oss-distributions/xnu/blob/main/libsyscall/wrappers/libproc/libproc.c
// sys/resource.h defines the fixed v2 layout. No task port or elevated rights
// are requested; denied/exited processes fall back to the sampled RSS.
func physicalFootprint(pid int32) (uint64, error) {
	var b [176]byte
	_, _, errno := syscall.Syscall6(unix.SYS_PROC_INFO, 9, uintptr(pid), 2, 0, uintptr(unsafe.Pointer(&b[0])), 0)
	if errno != 0 {
		return 0, errno
	}
	return parseFootprint(b[:])
}

func pressureLevelDarwin(n uint32) string {
	// The public sysctl exports VM pressure NOTE bits, not the internal enum.
	switch n {
	case 1:
		return "normal"
	case 2:
		return "warning"
	case 4:
		return "critical"
	}
	return "unknown"
}
