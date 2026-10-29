package command

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"golang.org/x/net/bpf"
)

func TestSeccompSocketPolicy(t *testing.T) {
	for _, arch := range []struct {
		name                string
		audit, socket, pair uint32
	}{
		{"amd64", 0xc000003e, 41, 53},
		{"arm64", 0xc00000b7, 198, 199},
	} {
		t.Run(arch.name, func(t *testing.T) {
			var raw []bpf.RawInstruction
			for _, ins := range socketFilter(arch.name) {
				raw = append(raw, bpf.RawInstruction{Op: ins.Code, Jt: ins.Jt, Jf: ins.Jf, K: ins.K})
			}
			ins, ok := bpf.Disassemble(raw)
			if !ok {
				t.Fatal("invalid BPF")
			}
			vm, err := bpf.NewVM(ins)
			if err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				name            string
				nr, abi, domain uint32
				want            int
			}{
				{"io_uring_setup", 425, arch.audit, 0, 0x50001},
				{"io_uring_enter", 426, arch.audit, 0, 0x50001},
				{"io_uring_register", 427, arch.audit, 0, 0x50001},
				{"unix_socket", arch.socket, arch.audit, 1, 0x50001},
				{"unix_socketpair", arch.pair, arch.audit, 1, 0x50001},
				{"inet_socket", arch.socket, arch.audit, 2, 0x7fff0000},
				{"inet6_socket", arch.socket, arch.audit, 10, 0x7fff0000},
				{"ordinary_syscall", 1, arch.audit, 0, 0x7fff0000},
				{"wrong_abi", arch.socket, 0, 2, 0x80000000},
				{"x32", 0x40000000 | arch.socket, arch.audit, 2, 0x80000000},
			} {
				t.Run(tc.name, func(t *testing.T) {
					// The VM loads big-endian packets. Encode seccomp_data's
					// scalar values accordingly to exercise the generated jumps.
					data := make([]byte, 64)
					binary.BigEndian.PutUint32(data, tc.nr)
					binary.BigEndian.PutUint32(data[4:], tc.abi)
					binary.BigEndian.PutUint32(data[16:], tc.domain)
					got, err := vm.Run(data)
					if err != nil || got != tc.want {
						t.Fatalf("action=%#x want=%#x: %v", got, tc.want, err)
					}
				})
			}
		})
	}
}

func TestSeccompSourceUnreachableFromWritableRoots(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux policy descriptors use Unix unlink semantics")
	}
	for _, broad := range []bool{false, true} {
		s := Scope{Project: t.TempDir(), Scratch: t.TempDir()}
		if broad {
			s.Project = os.TempDir()
		}
		var err error
		s.Project, err = canonical(s.Project)
		if err != nil {
			t.Fatal(err)
		}
		s.Scratch, err = canonical(s.Scratch)
		if err != nil {
			t.Fatal(err)
		}
		f, err := seccompFile(s)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close(); os.Remove(f.Name()) })
		path, err := canonical(f.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, root := range []string{s.Project, s.Scratch} {
			if within(path, root) {
				t.Errorf("policy source %s is inside writable root %s", path, root)
			}
		}
		if _, err := os.Stat(f.Name()); !os.IsNotExist(err) {
			t.Errorf("policy source still linked: %v", err)
		}
		st, err := f.Stat()
		if err != nil {
			t.Fatal(err)
		}
		if links := reflect.Indirect(reflect.ValueOf(st.Sys())).FieldByName("Nlink").Uint(); links != 0 {
			t.Errorf("policy has %d reachable links", links)
		}
		var first seccompInstruction
		if err := binary.Read(f, binary.NativeEndian, &first); err != nil {
			t.Fatal(err)
		}
		if first.Code != 0x20 || first.K != 4 {
			t.Fatal("descriptor is not positioned at policy start")
		}
		entries, err := filepath.Glob(filepath.Join(s.Scratch, ".seccomp-*"))
		if err != nil || len(entries) != 0 {
			t.Errorf("shared scratch exposes policy files: %v %v", entries, err)
		}
	}
}
