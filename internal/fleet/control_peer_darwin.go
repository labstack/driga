//go:build darwin

package fleet

import (
	"fmt"
	"golang.org/x/sys/unix"
	"net"
	"os"
)

func checkControlPeer(c *net.UnixConn) error {
	raw, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var credErr error
	err = raw.Control(func(fd uintptr) {
		cred, e := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if e != nil {
			credErr = e
			return
		}
		if cred.Uid != uint32(os.Getuid()) {
			credErr = fmt.Errorf("fleet control peer is not owned by this user")
		}
	})
	if err != nil {
		return err
	}
	return credErr
}
