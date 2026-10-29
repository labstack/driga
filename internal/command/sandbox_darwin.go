package command

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func nativeCapability(ctx context.Context) Capability {
	version, _ := unix.Sysctl("kern.osproductversion")
	c := Capability{Backend: "/usr/bin/sandbox-exec", OSVersion: version}
	if st, e := os.Stat(c.Backend); e != nil || !st.Mode().IsRegular() {
		c.Reason = "Seatbelt sandbox-exec is missing"
		c.Backend = ""
		return c
	}
	return probeBackend(ctx, c)
}

func sandboxProfile(p Prepared) string {
	// Default deny includes host signals, networking (including pathname Unix
	// sockets), mach services, process inspection and inherited IPC authority.
	var b strings.Builder
	b.WriteString("(version 1)\n(deny default)\n(allow process-exec process-fork sysctl-read)\n(allow signal (target self))\n(allow file-read* file-map-executable)\n")
	for _, root := range []string{p.Scope.Project, p.Scope.Scratch} {
		if root != "" {
			fmt.Fprintf(&b, "(allow file-write* (subpath %s))\n", strconv.Quote(root))
		}
	}
	b.WriteString("(allow file-write-data (literal \"/dev/null\"))\n")
	for _, root := range p.Scope.Denied {
		fmt.Fprintf(&b, "(deny file-read* file-write* (subpath %s))\n", strconv.Quote(root))
	}
	// A network grant is IP only: host Unix sockets never become an escape.
	if p.Parsed.Request.Network {
		b.WriteString("(allow network-outbound (remote ip))\n(allow network-inbound (local ip))\n")
	}
	return b.String()
}
func sandboxCommand(ctx context.Context, p Prepared, argv []string) (*exec.Cmd, func(), error) {
	args := append([]string{"-p", sandboxProfile(p), "--"}, argv...)
	return exec.CommandContext(ctx, p.Sandbox.Backend, args...), func() {}, nil
}
