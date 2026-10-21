package platform

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"
	"unicode/utf16"
)

func utf16Bytes(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[2*i:], c)
	}
	return b
}

func TestWindowsTimeAndMemory(t *testing.T) {
	swap := winMachine(winMemory{SwapTotal: 100, SwapUsed: 30}, 2)
	if swap.SwapTotal != 100 || swap.SwapUsed != 30 {
		t.Fatal(swap)
	}
	if got := winTime(116444736000000000 + 12345678); !got.Equal(time.Unix(1, 234567800)) {
		t.Fatal(got)
	}
	s := winMachine(winMemory{TotalPhys: 100, AvailPhys: 40, TotalPageFile: 150, AvailPageFile: 30}, 4)
	if s.MemTotal != 100 || s.MemUsed != 60 || s.CommitLimit != 150 || s.CommitUsed != 120 || s.Load1 != -1 || s.PressureLevel != "unknown" {
		t.Fatalf("%+v", s)
	}
	if winMachine(winMemory{TotalPhys: 1, AvailPhys: 2}, 1).MemUsed != 0 {
		t.Fatal("underflow")
	}
}

func TestNTString(t *testing.T) {
	for _, width := range []int{4, 8} {
		text := `"C:\Program Files\agent.exe" --name 子🙂`
		data := utf16Bytes(text)
		b := make([]byte, 16+len(data))
		copy(b[16:], data)
		binary.LittleEndian.PutUint16(b, uint16(len(data)))
		binary.LittleEndian.PutUint16(b[2:], uint16(len(data)))
		if width == 4 {
			binary.LittleEndian.PutUint32(b[4:], 0x1010)
		} else {
			binary.LittleEndian.PutUint64(b[8:], 0x1010)
		}
		got, err := winNTString(b, 0x1000, width)
		if err != nil || got != text {
			t.Fatalf("%q %v", got, err)
		}
		if _, err := winNTString(b[:16], 0x1000, width); err == nil {
			t.Fatal("out-of-buffer string accepted")
		}
		b[0] = 1
		if _, err := winNTString(b, 0x1000, width); err == nil {
			t.Fatal("odd UTF16 accepted")
		}
	}
}

func TestRemoteCwdNativeAndWOW64(t *testing.T) {
	for _, width := range []int{4, 8} {
		peb := make([]byte, 0x28)
		params := make([]byte, 0x50)
		put := func(b []byte, off int, n uint64) {
			if width == 4 {
				binary.LittleEndian.PutUint32(b[off:], uint32(n))
			} else {
				binary.LittleEndian.PutUint64(b[off:], n)
			}
		}
		po, co := 0x10, 0x24
		if width == 8 {
			po, co = 0x20, 0x38
		}
		put(peb, po, 0x2000)
		cwd := `C:\Work\子 folder\`
		text := utf16Bytes(cwd)
		binary.LittleEndian.PutUint16(params[co:], uint16(len(text)))
		binary.LittleEndian.PutUint16(params[co+2:], uint16(len(text)))
		pointerOff := co + 4
		if width == 8 {
			pointerOff = co + 8
		}
		put(params, pointerOff, 0x3000)
		memory := map[uint64][]byte{0x1000: peb, 0x2000: params, 0x3000: text}
		read := func(addr uint64, n uint32) ([]byte, error) {
			b := memory[addr]
			if len(b) < int(n) {
				return nil, errors.New("denied")
			}
			return b[:n], nil
		}
		got, err := winCwd(0x1000, width, read)
		if err != nil || got != cwd {
			t.Fatalf("%q %v", got, err)
		}
		delete(memory, 0x3000)
		if _, err := winCwd(0x1000, width, read); err == nil {
			t.Fatal("read failure guessed cwd")
		}
	}
}

func TestWindowsPartialAndCancellation(t *testing.T) {
	p := winPlatform{snapshot: func() ([]winEntry, error) { return []winEntry{{PID: 42, PPID: 7, Exe: "agent.exe"}}, nil }, inspect: func(int32) (winDetails, error) { return winDetails{}, errors.New("denied") }}
	ps, err := p.Procs(context.Background())
	if err != nil || len(ps) != 1 || ps[0].PID != 42 || ps[0].Exe != "agent.exe" || !ps[0].Partial || ps[0].Cmdline != "" {
		t.Fatalf("%+v %v", ps, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Procs(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestWindowsSnapshotRetainsUnknownButOmitsExitedProcesses(t *testing.T) {
	// Toolhelp still lists a terminated child while an open handle keeps its
	// process object around. Before this fix the second pass emitted PID 42
	// with a zero start, making the kill guard report a false identity change.
	exited := false
	const ticks = 116444736000000000 + 12345678
	p := winPlatform{
		snapshot: func() ([]winEntry, error) {
			return []winEntry{{PID: 42, PPID: 7}, {PID: 43, PPID: 7}}, nil
		},
		inspect: func(pid int32) (winDetails, error) {
			if pid == 43 {
				return winDetails{}, errors.New("access denied")
			}
			if exited {
				return winDetails{}, errWinProcessGone
			}
			return winDetails{Start: ticks}, nil
		},
	}
	before, err := p.Procs(context.Background())
	if err != nil || len(before) != 2 || !before[0].StartTime.Equal(winTime(ticks)) {
		t.Fatalf("before: %+v %v", before, err)
	}
	exited = true
	after, err := p.Procs(context.Background())
	if err != nil || len(after) != 1 || after[0].PID != 43 || !after[0].Partial || !after[0].StartTime.IsZero() {
		t.Fatalf("after: %+v %v", after, err)
	}
}
