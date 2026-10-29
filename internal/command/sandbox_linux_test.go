package command

import (
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"testing"
)

func TestBubblewrapPolicyDescriptors(t *testing.T) {
	s := runnerScope(t)
	for _, root := range []string{s.Project, s.Scratch} {
		if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	cmd, cleanup, err := sandboxCommand(context.Background(), Prepared{Scope: s, Sandbox: Capability{Backend: "/usr/bin/bwrap"}}, []string{"/usr/bin/true"})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	// ExtraFiles[0] is child fd 3, regardless of its descriptor in this process.
	// Read the sources from their current offsets, as bwrap 0.9 does.
	var filter []seccompInstruction
	for i, arg := range cmd.Args {
		if arg == "--seccomp" {
			fd, err := strconv.Atoi(cmd.Args[i+1])
			if err != nil || fd != 3 {
				t.Fatal(fd, err)
			}
			filter = make([]seccompInstruction, len(socketFilter(runtime.GOARCH)))
			if err := binary.Read(cmd.ExtraFiles[fd-3], binary.NativeEndian, filter); err != nil {
				t.Fatal(err)
			}
		}
		if arg == "--ro-bind-data" {
			fd, err := strconv.Atoi(cmd.Args[i+1])
			if err != nil || fd < 4 || fd-3 >= len(cmd.ExtraFiles) {
				t.Fatal(fd, err)
			}
			data, err := io.ReadAll(cmd.ExtraFiles[fd-3])
			if err != nil || string(data) != ".\n" {
				t.Fatalf("fd %d: %q %v", fd, data, err)
			}
		}
	}
	if !reflect.DeepEqual(filter, socketFilter(runtime.GOARCH)) || len(cmd.ExtraFiles) != 3 {
		t.Fatal(filter, len(cmd.ExtraFiles))
	}
}
