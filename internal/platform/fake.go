package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/labstack/driga/internal/pathx"
	"os"
	"path/filepath"
	"time"

	"github.com/labstack/driga/internal/model"
)

// Fake supplies a fixed process table and machine sample without host access.
// LoadFake reads procs.json and machine.json with an explicit OS. Optional
// sockets.json maps pids to serving flags; peers.json maps pids to peer pids.
type Fake struct {
	procs   []model.Proc
	machine model.MachineSample
	os      string
	sockets map[int32]bool
	peers   map[int32][]int32
	// subreaper comes from an optional subreaper.json {"uid":N} plus a
	// synthetic proc/ tree beside the fixture.
	procRoot  string
	uid       *int
	openFiles []string
}

var _ SubreaperFinder = (*Fake)(nil)

// UserSubreaper resolves the fixture's systemd --user instance, if any.
func (f *Fake) UserSubreaper() (int32, bool) {
	if f.uid == nil {
		return 0, false
	}
	return UserSubreaper(f.procRoot, *f.uid)
}

var _ Platform = (*Fake)(nil)
var _ SocketProber = (*Fake)(nil)

// LoadFake reads a fixture table. machine.json must name the source OS;
// malformed or unreadable files are errors, including optional files.
func LoadFake(dir string) (*Fake, error) {
	f := &Fake{}
	read := func(name string, dst any, optional bool) error {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if optional && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("fake %s: %w", name, err)
		}
		if err := json.Unmarshal(b, dst); err != nil {
			return fmt.Errorf("fake %s: %w", name, err)
		}
		return nil
	}
	if err := read("procs.json", &f.procs, false); err != nil {
		return nil, err
	}
	var machine struct {
		model.MachineSample
		OS string `json:"os"`
	}
	if err := read("machine.json", &machine, false); err != nil {
		return nil, err
	}
	if machine.OS == "" {
		return nil, fmt.Errorf("fake machine.json: source OS is required")
	}
	f.machine, f.os = machine.MachineSample, machine.OS
	if err := read("sockets.json", &f.sockets, true); err != nil {
		return nil, err
	}
	if err := read("peers.json", &f.peers, true); err != nil {
		return nil, err
	}
	if err := read("open-files.json", &f.openFiles, true); err != nil {
		return nil, err
	}
	seen := map[int32]bool{}
	for _, p := range f.procs {
		if p.PID <= 0 || p.StartTime.IsZero() || seen[p.PID] {
			return nil, fmt.Errorf("fake procs.json: invalid or duplicate process identity %d", p.PID)
		}
		seen[p.PID] = true
	}
	var sub struct {
		UID *int `json:"uid"`
	}
	if err := read("subreaper.json", &sub, true); err != nil {
		return nil, err
	}
	f.uid, f.procRoot = sub.UID, filepath.Join(dir, "proc")
	return f, nil
}

// ProcessOS returns explicit fixture provenance, never the replay host's OS.
func (f *Fake) ProcessOS() string { return f.os }

func (f *Fake) OpenFiles(ctx context.Context, path string) (bool, error) {
	for _, p := range f.openFiles {
		if pathx.RecordedWithin(p, path) {
			return true, ctx.Err()
		}
	}
	return false, ctx.Err()
}

// ExternalPeers returns optional fixture sockets/listeners and peer pid links.
func (f *Fake) ExternalPeers(ctx context.Context, pids []int32) (map[int32]bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	wanted := make(map[int32]bool, len(pids))
	for _, pid := range pids {
		wanted[pid] = true
	}
	result := make(map[int32]bool, len(pids))
	for _, pid := range pids {
		result[pid] = f.sockets[pid]
		for _, peer := range f.peers[pid] {
			if !wanted[peer] {
				result[pid] = true
			}
		}
	}
	return result, nil
}

// Procs returns a copy, since collectors enrich the process table in place.
func (f *Fake) Procs(ctx context.Context) ([]model.Proc, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]model.Proc(nil), f.procs...), nil
}

// Machine returns the fixed machine sample.
func (f *Fake) Machine(ctx context.Context) (model.MachineSample, error) {
	return f.machine, ctx.Err()
}

// Cwds returns only requested, present process working directories.
func (f *Fake) Cwds(ctx context.Context, pids []int32) (map[int32]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	wanted := make(map[int32]bool, len(pids))
	for _, pid := range pids {
		wanted[pid] = true
	}
	cwds := make(map[int32]string)
	for _, p := range f.procs {
		if wanted[p.PID] && p.Cwd != "" {
			cwds[p.PID] = p.Cwd
		}
	}
	return cwds, nil
}

// Alive requires both the pid and exact start timestamp to match the fixture.
func (f *Fake) Alive(pid int32, start time.Time) bool {
	for _, p := range f.procs {
		if p.PID == pid {
			return !start.IsZero() && p.StartTime.Equal(start)
		}
	}
	return false
}
