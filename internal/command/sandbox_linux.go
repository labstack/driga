package command

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"golang.org/x/sys/unix"
)

func nativeCapability(ctx context.Context) Capability {
	var u unix.Utsname
	_ = unix.Uname(&u)
	var version []byte
	for _, c := range u.Release {
		if c == 0 {
			break
		}
		version = append(version, byte(c))
	}
	c := Capability{OSVersion: string(version)}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		c.Reason = "Sandbox seccomp architecture is unsupported"
		return c
	}
	for _, path := range []string{"/usr/bin/bwrap", "/bin/bwrap"} {
		if st, e := os.Stat(path); e == nil && st.Mode().IsRegular() {
			c.Backend = path
			break
		}
	}
	if c.Backend == "" {
		c.Reason = "Running commands needs bubblewrap (bwrap), which is missing."
		return c
	}
	return probeBackend(ctx, c)
}

func sandboxProfile(p Prepared) (string, error) {
	args, err := bubbleArgs(p)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(struct {
		Args   []string
		Arch   string
		Filter string
	}{args, runtime.GOARCH, "deny-unix-sockets-and-io-uring-v2"})
	return string(b), nil
}

func sandboxCommand(ctx context.Context, p Prepared, argv []string) (*exec.Cmd, func(), error) {
	args, err := bubbleArgs(p)
	if err != nil {
		return nil, func() {}, err
	}
	remove, err := bubblePlaceholders(args)
	if err != nil {
		return nil, func() {}, err
	}
	f, err := seccompFile(p.Scope)
	if err != nil {
		remove()
		return nil, func() {}, err
	}
	cleanup := func() { f.Close(); remove() }
	cmd := exec.CommandContext(ctx, p.Sandbox.Backend, append(args, argv...)...)
	// bwrap consumes these private policy descriptors before exec; no caller
	// descriptors or sockets are inherited.
	cmd.ExtraFiles = []*os.File{f}
	for _, arg := range args {
		if arg != "--ro-bind-data" {
			continue
		}
		data, err := os.CreateTemp("", "driga-git-commondir-")
		if err != nil {
			cleanup()
			return nil, func() {}, err
		}
		if err = os.Remove(data.Name()); err == nil {
			_, err = data.WriteString(".\n")
		}
		if err == nil {
			_, err = data.Seek(0, 0)
		}
		if err != nil {
			data.Close()
			cleanup()
			return nil, func() {}, err
		}
		cmd.ExtraFiles = append(cmd.ExtraFiles, data)
		oldCleanup := cleanup
		cleanup = func() { data.Close(); oldCleanup() }
	}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		cleanup()
		return nil, func() {}, fmt.Errorf("unsupported sandbox ABI")
	}
	return cmd, cleanup, nil
}
