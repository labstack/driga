package command

import (
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
)

// seccompInstruction is the kernel sock_filter wire layout. Keeping the
// generated policy portable allows its behavior to be tested on every host.
type seccompInstruction struct {
	Code   uint16
	Jt, Jf uint8
	K      uint32
}

func socketFilter(architecture string) []seccompInstruction {
	arch, nrSocket, nrPair := uint32(0xc000003e), uint32(41), uint32(53)
	if architecture == "arm64" {
		arch, nrSocket, nrPair = 0xc00000b7, 198, 199
	}
	// Kill on ABI mismatch/x32; deny AF_UNIX socket creation and socketpair.
	// io_uring can create sockets without socket(2), so deny its entire API.
	// These three syscall numbers are shared by amd64 and arm64.
	return []seccompInstruction{
		{Code: 0x20, K: 4}, {Code: 0x15, Jt: 1, K: arch}, {Code: 0x06, K: 0x80000000},
		{Code: 0x20, K: 0}, {Code: 0x35, Jf: 1, K: 0x40000000}, {Code: 0x06, K: 0x80000000},
		{Code: 0x15, Jf: 1, K: 425}, {Code: 0x06, K: 0x00050001},
		{Code: 0x15, Jf: 1, K: 426}, {Code: 0x06, K: 0x00050001},
		{Code: 0x15, Jf: 1, K: 427}, {Code: 0x06, K: 0x00050001},
		{Code: 0x15, Jt: 2, K: nrSocket}, {Code: 0x15, Jt: 1, K: nrPair}, {Code: 0x06, K: 0x7fff0000},
		{Code: 0x20, K: 16}, {Code: 0x15, Jf: 1, K: 1}, {Code: 0x06, K: 0x00050001}, {Code: 0x06, K: 0x7fff0000},
	}
}

// seccompFile builds the policy descriptor consumed by bubblewrap.
func seccompFile(s Scope) (*os.File, error) {
	// Never publish a policy inode in a bind-mounted root, even briefly. The
	// child's /tmp is a separate tmpfs; host temporary directories are absent.
	// Unlink before writing and retain only the private descriptor. Concurrent
	// sandboxed commands then have neither a pathname nor an inherited FD to it.
	var f *os.File
	var err error
	for _, dir := range []string{os.TempDir(), "/tmp", "/var/tmp"} {
		dir, err = canonical(dir)
		if err != nil || within(dir, s.Project) || within(dir, s.Scratch) {
			continue
		}
		f, err = os.CreateTemp(dir, "driga-seccomp-")
		if err == nil {
			break
		}
	}
	if f == nil {
		return nil, fmt.Errorf("no private seccomp source directory: %v", err)
	}
	if err = os.Remove(f.Name()); err != nil {
		f.Close()
		return nil, err
	}
	fail := func(err error) (*os.File, error) { f.Close(); return nil, err }
	if err = binary.Write(f, binary.NativeEndian, socketFilter(runtime.GOARCH)); err != nil {
		return fail(err)
	}
	if _, err = f.Seek(0, 0); err != nil {
		return fail(err)
	}
	return f, nil
}
