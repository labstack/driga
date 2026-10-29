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

func bubbleArgs(p Prepared) []string {
	a := []string{"--unshare-all", "--disable-userns", "--new-session", "--die-with-parent", "--cap-drop", "ALL"}
	if p.Parsed.Request.Network {
		a = append(a, "--share-net")
	}
	// Build a fresh root. Host /run, /tmp, home and pathname sockets are absent.
	for _, dir := range []string{"/usr", "/bin", "/sbin", "/lib", "/lib64"} {
		if _, e := os.Stat(dir); e == nil {
			a = append(a, "--ro-bind", dir, dir)
		}
	}
	a = append(a, "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp")
	for _, file := range []string{"/etc/resolv.conf", "/etc/hosts", "/etc/nsswitch.conf", "/etc/ld.so.cache"} {
		if _, e := os.Stat(file); e == nil {
			a = append(a, "--ro-bind", file, file)
		}
	}
	for _, root := range []string{p.Scope.Project, p.Scope.Scratch} {
		if root != "" {
			a = append(a, "--bind", root, root)
		}
	}
	for _, root := range p.Scope.Denied {
		// Only roots made visible by a bind need masking; absent paths stay absent.
		visible := within(root, p.Scope.Project) || within(root, p.Scope.Scratch) || within(root, "/usr")
		if !visible {
			continue
		}
		if st, e := os.Stat(root); e == nil {
			if st.IsDir() {
				a = append(a, "--tmpfs", root, "--remount-ro", root)
			} else {
				a = append(a, "--ro-bind", "/dev/null", root)
			}
		}
	}
	return append(a, "--chdir", p.Parsed.Request.Cwd, "--seccomp", "3", "--")
}
func sandboxProfile(p Prepared) string {
	b, _ := json.Marshal(struct {
		Args   []string
		Arch   string
		Filter string
	}{bubbleArgs(p), runtime.GOARCH, "deny-unix-sockets-and-io-uring-v2"})
	return string(b)
}

func sandboxCommand(ctx context.Context, p Prepared, argv []string) (*exec.Cmd, func(), error) {
	f, err := seccompFile(p.Scope)
	if err != nil {
		return nil, func() {}, err
	}
	cleanup := func() { f.Close() }
	cmd := exec.CommandContext(ctx, p.Sandbox.Backend, append(bubbleArgs(p), argv...)...)
	// This is the sole intentional extra descriptor. bwrap consumes/closes the
	// seccomp program before exec; no caller descriptors or sockets are inherited.
	cmd.ExtraFiles = []*os.File{f}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		cleanup()
		return nil, func() {}, fmt.Errorf("unsupported sandbox ABI")
	}
	return cmd, cleanup, nil
}
