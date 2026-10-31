package command

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/labstack/driga/internal/kill"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
)

type processSelector struct {
	pids     []int32
	names    []string
	pattern  *regexp.Regexp
	full     bool
	fold     bool
	stripExe bool
}

func processName(s string) string { return filepath.Base(strings.ReplaceAll(s, "\\", "/")) }
func processPID(s string) (int32, error) {
	n, e := strconv.ParseInt(s, 10, 32)
	if e != nil || n <= 1 {
		return 0, fmt.Errorf("only positive individual PIDs greater than 1 are supported")
	}
	return int32(n), nil
}
func processSelection(p Parsed) (processSelector, error) {
	var s processSelector
	bad := func() (processSelector, error) {
		return s, fmt.Errorf("unsupported process selector; groups, force, wrappers and ambiguous patterns are refused")
	}
	if p.Complex || p.Pipeline || len(p.Commands) != 1 || len(p.Commands[0]) < 2 {
		return bad()
	}
	a := p.Commands[0]
	name := strings.TrimSuffix(strings.ToLower(processName(a[0])), ".exe")
	a = a[1:]
	addPID := func(v string) error {
		n, e := processPID(v)
		if e == nil {
			s.pids = append(s.pids, n)
		}
		return e
	}
	switch name {
	case "kill":
		if len(a) > 0 && (a[0] == "-TERM" || a[0] == "-SIGTERM" || a[0] == "-15") {
			a = a[1:]
		} else if len(a) > 1 && a[0] == "-s" && (a[1] == "TERM" || a[1] == "SIGTERM" || a[1] == "15") {
			a = a[2:]
		}
		if len(a) > 0 && a[0] == "--" {
			a = a[1:]
		}
		if len(a) == 0 {
			return bad()
		}
		for _, v := range a {
			if e := addPID(v); e != nil {
				return bad()
			}
		}
	case "pkill":
		if len(a) > 0 && a[0] == "-f" {
			s.full = true
			a = a[1:]
		}
		if len(a) > 0 && a[0] == "--" {
			a = a[1:]
		}
		if len(a) != 1 || a[0] == "" || strings.HasPrefix(a[0], "-") {
			return bad()
		}
		var e error
		s.pattern, e = regexp.CompilePOSIX(a[0])
		if e != nil {
			return bad()
		}
	case "killall":
		if len(a) > 0 && a[0] == "--" {
			a = a[1:]
		}
		if len(a) == 0 {
			return bad()
		}
		for _, v := range a {
			if v == "" || strings.HasPrefix(v, "-") || strings.ContainsAny(v, "/\\*?[") {
				return bad()
			}
			s.names = append(s.names, v)
		}
	case "taskkill":
		s.fold = true
		if len(a)%2 != 0 {
			return bad()
		}
		var kind string
		for i := 0; i < len(a); i += 2 {
			flag := strings.ToLower(a[i])
			if kind != "" && flag != kind {
				return bad()
			}
			kind = flag
			switch flag {
			case "/pid":
				if e := addPID(a[i+1]); e != nil {
					return bad()
				}
			case "/im":
				if strings.ContainsAny(a[i+1], "*?/\\[") || a[i+1] == "" {
					return bad()
				}
				s.names = append(s.names, a[i+1])
			default:
				return bad()
			}
		}
	case "stop-process":
		s.fold = true
		s.stripExe = true
		if len(a) != 2 {
			return bad()
		}
		switch strings.ToLower(a[0]) {
		case "-id":
			for _, v := range strings.Split(a[1], ",") {
				if e := addPID(v); e != nil {
					return bad()
				}
			}
		case "-name":
			if a[1] == "" || strings.ContainsAny(a[1], "*?/\\[, ") {
				return bad()
			}
			s.names = []string{a[1]}
		default:
			return bad()
		}
	default:
		return bad()
	}
	return s, nil
}

func ResolveProcessTargets(ctx context.Context, plat platform.Platform, p Parsed, protected []model.Proc) ([]model.Proc, error) {
	s, err := processSelection(p)
	if err != nil {
		return nil, err
	}
	ps, err := plat.Procs(ctx)
	if err != nil {
		return nil, err
	}
	var targets []model.Proc
	seen := map[int32]bool{}
	protection := kill.NewProtection(ps, protected)
	for _, proc := range ps {
		name := processName(proc.Exe)
		match := slices.Contains(s.pids, proc.PID)
		for _, v := range s.names {
			matched := v == name
			if s.fold {
				candidate := name
				if s.stripExe {
					candidate = strings.TrimSuffix(strings.ToLower(candidate), ".exe")
				}
				matched = strings.EqualFold(v, candidate)
			}
			if matched {
				match = true
			}
		}
		if s.pattern != nil {
			text := name
			if s.full {
				text = proc.Cmdline
			}
			match = s.pattern.MatchString(text)
		}
		if !match || seen[proc.PID] {
			continue
		}
		if reason := protection.Reason(proc); reason != "" {
			return nil, fmt.Errorf("PID %d: %s", proc.PID, reason)
		}
		if proc.Zombie {
			continue
		}
		seen[proc.PID] = true
		proc.Argv = append([]string(nil), proc.Argv...)
		targets = append(targets, proc)
	}
	// An explicit missing PID is not silently replaced by a partial target set.
	for _, pid := range s.pids {
		if !seen[pid] {
			return nil, fmt.Errorf("PID %d vanished or is unavailable", pid)
		}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no matching eligible processes")
	}
	slices.SortFunc(targets, func(a, b model.Proc) int { return int(a.PID) - int(b.PID) })
	return targets, nil
}

func snapshotTarget(p model.Proc) ProcessTarget {
	p.Argv = append([]string(nil), p.Argv...)
	return ProcessTarget{Proc: p, OwnerUID: p.OwnerUID, OwnerSID: p.OwnerSID, OwnerKnown: p.OwnerKnown}
}
func targetProc(t ProcessTarget) model.Proc {
	p := t.Proc
	p.Argv = append([]string(nil), p.Argv...)
	p.OwnerUID = t.OwnerUID
	p.OwnerSID = t.OwnerSID
	p.OwnerKnown = t.OwnerKnown
	return p
}

// PrepareProcessTargets takes a fresh live snapshot for the initial TERM card.
// It is a broker operation; a model-supplied snapshot never enters this path.
func PrepareProcessTargets(ctx context.Context, plat platform.Platform, p Prepared, protected []model.Proc) (Prepared, error) {
	if e := validatePrepared(p); e != nil {
		return Prepared{}, e
	}
	if p.Policy.Class != Process || p.Parsed.Request.Network {
		return Prepared{}, fmt.Errorf("not a local process request")
	}
	targets, e := ResolveProcessTargets(ctx, plat, p.Parsed, protected)
	if e != nil {
		return Prepared{}, e
	}
	s := &ProcessSnapshot{}
	for _, t := range targets {
		s.Targets = append(s.Targets, snapshotTarget(t))
	}
	for _, t := range protected {
		s.Protected = append(s.Protected, snapshotTarget(t))
	}
	p.ProcessSnapshot = s
	p.seal = Digest(p)
	return p, nil
}

// PrepareForce narrows a second card to exact surviving identities from the
// first. The broker must obtain a new person decision for this distinct digest.
func PrepareForce(p Prepared, survivors []model.Proc) (Prepared, error) {
	if e := validatePrepared(p); e != nil {
		return Prepared{}, e
	}
	if p.ProcessSnapshot == nil || p.ProcessSnapshot.Force || len(survivors) == 0 {
		return Prepared{}, fmt.Errorf("force requires an initial snapshot and survivors")
	}
	s := &ProcessSnapshot{Force: true, Protected: append([]ProcessTarget(nil), p.ProcessSnapshot.Protected...)}
	for _, v := range survivors {
		found := false
		for _, old := range p.ProcessSnapshot.Targets {
			if kill.SameIdentity(targetProc(old), v) {
				found = true
				break
			}
		}
		if !found {
			return Prepared{}, fmt.Errorf("survivor was not in the approved snapshot")
		}
		s.Targets = append(s.Targets, snapshotTarget(v))
	}
	p.ProcessSnapshot = s
	p.seal = Digest(p)
	return p, nil
}
