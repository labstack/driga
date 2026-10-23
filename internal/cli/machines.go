package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

// MachinesCmd lists cached fleet state, including when collection has stopped.
type MachinesCmd struct {
	List    MachinesListCmd   `cmd:"" default:"withargs" help:"List known machines and connection health."`
	Show    MachineShowCmd    `cmd:"" help:"Show one machine (default: local)."`
	ResetID MachineResetIDCmd `cmd:"" name:"reset-id" help:"Regenerate the local machine identity."`
}
type MachinesListCmd struct{ MachineListOptions }

// Run renders known machines without requiring a fresh collector pass.
func (MachinesListCmd) Run(g *Globals) error {
	copy := *g
	copy.cachedMachines = true
	return runRead(&copy, view.Filter{}, "", func(r view.Reader, f view.Filter, now time.Time) (any, error) {
		v, e := view.Machines(r, now, f.Scope)
		if !g.Snapshot {
			if !collector.Running(config.DataDir()) {
				v.Warning = "viewer collector not running"
			}
		}
		return v, e
	})
}

func resolveMachine(g *Globals, r view.Reader) (store.Scope, error) {
	input := g.Machine
	switch input {
	case "", "all":
		return store.Scope{}, nil
	case "local":
		return store.Scope{Machines: []string{r.MachineID()}}, nil
	}
	ms, e := r.Machines()
	if e != nil {
		return store.Scope{}, e
	}
	var ids, names []string
	for _, m := range ms {
		names = append(names, view.MachineName(m))
		if input == m.MachineID || input == m.Name || input == m.Hostname || m.DeviceID != "" && strings.HasPrefix(strings.ToUpper(m.DeviceID), strings.ToUpper(input)) {
			ids = append(ids, m.MachineID)
		}
	}
	if len(ids) == 1 {
		return store.Scope{Machines: ids}, nil
	}
	sort.Strings(names)
	if len(ids) > 1 {
		return store.Scope{}, fmt.Errorf("ambiguous machine %q; available: %s", input, strings.Join(names, ", "))
	}
	return store.Scope{}, fmt.Errorf("unknown machine %q; available: local, all, %s", input, strings.Join(names, ", "))
}

func resolveViewFilter(g *Globals, r view.Reader, f view.Filter, project string) (view.Filter, error) {
	scope, e := resolveMachine(g, r)
	if e != nil {
		return f, e
	}
	f.Scope = scope
	if project == "" {
		return f, nil
	}
	// Dot always names a repository on the viewer, even when a peer has the same path.
	local := project == "."
	if scope.Machines == nil {
		if _, e := os.Stat(config.ExpandHome(project)); e == nil {
			local = true
		}
	}
	if local {
		if scope.Machines != nil {
			ok := false
			for _, id := range scope.Machines {
				ok = ok || id == r.MachineID()
			}
			if !ok {
				return f, fmt.Errorf("project %q resolves locally; --machine excludes local", project)
			}
		}
		f.Scope = store.Scope{Machines: []string{r.MachineID()}}
		f.Project, e = resolveProject(r.ReadMachine(r.MachineID()), project)
		return f, e
	}
	ms, e := r.Machines()
	if e != nil {
		return f, e
	}
	// Local fixtures and early snapshots may not yet have a machines row.
	ids := []string{r.MachineID()}
	for _, m := range ms {
		if m.MachineID != r.MachineID() {
			ids = append(ids, m.MachineID)
		}
	}
	var paths, matches []string
	for _, id := range ids {
		if scope.Machines != nil {
			ok := false
			for _, want := range scope.Machines {
				ok = ok || id == want
			}
			if !ok {
				continue
			}
		}
		p, err := resolveProject(r.ReadMachine(id), project)
		if err != nil {
			if !strings.HasPrefix(err.Error(), "unknown project") {
				return f, err
			}
			continue
		}
		paths = append(paths, p)
		matches = append(matches, id)
	}
	if len(matches) == 0 {
		if strings.ContainsAny(project, "/\\") {
			return f, fmt.Errorf("unknown project path %q", project)
		}
		return f, fmt.Errorf("unknown project %q in selected machines", project)
	}
	if len(matches) > 1 {
		return f, fmt.Errorf("ambiguous project %q on machines %s; use --machine", project, strings.Join(matches, ", "))
	}
	f.Project = paths[0]
	f.Scope = store.Scope{Machines: matches}
	return f, nil
}
