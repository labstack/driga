//go:build !linux && !darwin

package fleet

import "net"

// Filesystem owner checks protect platforms without a peer credential API.
func checkControlPeer(*net.UnixConn) error { return nil }
