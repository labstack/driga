package derive

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
)

// MCPServerView is derived solely from one existing process table. A roll-up
// includes singles; only duplicate commands within one host are findings.
type MCPServerView struct {
	Pileups  []MCPPileup            `json:"pileups"`
	Commands []MCPCommand           `json:"commands"`
	Trends   []MCPTrend             `json:"trends"`
	Samples  []model.MCPObservation `json:"-"`
}

type MCPHost = model.MCPHost

type MCPCommand struct {
	Key              string     `json:"command_key"`
	Trends           []MCPTrend `json:"trends"`
	Name             string     `json:"name"`
	Command          string     `json:"command"`
	Commands         []string   `json:"commands"`
	Installs         []string   `json:"installs"`
	IncludesLauncher bool       `json:"includes_launcher"`
	DuplicateCount   int        `json:"duplicate_count"`
	Count            int        `json:"count"`
	Memory           uint64     `json:"memory_bytes"`
	MemoryMeasure    string     `json:"memory_measure"`
	OldestStart      time.Time  `json:"oldest_start"`
	NewestStart      time.Time  `json:"newest_start"`
	HostCount        int        `json:"host_count"`
	PileupCount      int        `json:"pileup_count"`
}

type MCPPileup struct {
	MCPCommand
	Host   MCPHost `json:"host"`
	Advice string  `json:"advice"`
}

// ProcessMemory follows the Machine view's physical footprint / RSS+swap / RSS
// fallback, with a measure label so totals never silently imply precision.
func ProcessMemory(p model.Proc) (uint64, string) {
	switch p.MemoryMeasure {
	case "physical_footprint", "private_bytes":
		return p.Footprint, p.MemoryMeasure
	case "rss_swap":
		return p.RSS + p.Swap, p.MemoryMeasure
	default:
		return p.RSS, "rss"
	}
}

func MCPServers(procs []model.Proc, sigs *signatures.Set, now time.Time) MCPServerView {
	result := MCPServerView{Pileups: []MCPPileup{}, Commands: []MCPCommand{}}
	if sigs == nil {
		sigs = signatures.Builtin()
	}
	byPID := make(map[int32]model.Proc, len(procs))
	identities := map[int32]mcpIdentity{}
	packages := map[string]string{}
	hosts := map[int32]MCPHost{}
	for _, p := range procs {
		if p.PID <= 0 || p.Zombie {
			continue
		}
		byPID[p.PID] = p
		d := describeMCP(p, sigs)
		identities[p.PID] = d
		if d.pkg != "" {
			bin := path.Base(d.pkg)
			if old, ok := packages[bin]; ok && old != d.pkg {
				packages[bin] = ""
			} else if !ok {
				packages[bin] = d.pkg
			}
		}
		sig, ok := sigs.Match(p.Cmdline, p.Exe)
		role, agent := sig.Role, sig.Agent
		if p.Agent != "" && p.Role != "" {
			role, agent = signatures.Role(p.Role), p.Agent
			ok = true
		}
		if !ok || agent == "" || (role != signatures.RoleInteractive && role != signatures.RoleHeadless && role != signatures.RoleHost && role != signatures.RoleService) {
			continue
		}
		age := time.Duration(0)
		if !p.StartTime.IsZero() {
			age = max(0, now.Sub(p.StartTime))
		}
		hosts[p.PID] = MCPHost{PID: p.PID, Name: sigs.ProcessName(p.Cmdline, p.Exe), Exe: p.Exe, Cwd: p.Cwd, StartedAt: p.StartTime, Age: age}
	}
	// A global bin shim and a scoped package can identify the same server.
	// Resolve that alias only when the observed package name is unambiguous.
	for pid, d := range identities {
		if strings.HasPrefix(d.key, "npm:") && d.pkg == "" {
			if pkg := packages[strings.TrimPrefix(d.key, "npm:")]; pkg != "" {
				d.key = "npm:" + pkg
			}
		}
		identities[pid] = d
	}
	// Memoize ancestry so deep wrapper trees cost O(processes), including cycles.
	owners := map[int32]int32{}
	visiting := map[int32]bool{}
	var owner func(int32) int32
	owner = func(pid int32) int32 {
		if host, ok := owners[pid]; ok {
			return host
		}
		if visiting[pid] {
			return 0
		}
		visiting[pid] = true
		p := byPID[pid]
		var host int32
		parent, ok := byPID[p.PPID]
		if ok && (p.StartTime.IsZero() || parent.StartTime.IsZero() || !parent.StartTime.After(p.StartTime)) {
			if _, ok := hosts[parent.PID]; ok {
				host = parent.PID
			} else {
				host = owner(parent.PID)
			}
		}
		visiting[pid] = false
		owners[pid] = host
		return host
	}
	children := map[int32][]int32{}
	for _, p := range byPID {
		children[p.PPID] = append(children[p.PPID], p.PID)
	}
	collapsed := map[int32]int32{}
	for pid, p := range byPID {
		kids := children[pid]
		if len(kids) != 1 {
			continue
		}
		child := byPID[kids[0]]
		if _, host := hosts[pid]; host {
			continue
		}
		if _, host := hosts[child.PID]; host {
			continue
		}
		if owner(pid) == 0 || owner(pid) != owner(child.PID) {
			continue
		}
		if !p.StartTime.IsZero() && !child.StartTime.IsZero() && p.StartTime.After(child.StartTime) {
			continue
		}
		if launchesMCP(identities[pid], identities[child.PID]) {
			collapsed[pid] = child.PID
		}
	}
	// A bin path alone loses a package's scope; a proven npm launcher supplies
	// that identity without changing the child's display label.
	binAliases := map[string]string{}
	for pid, childPID := range collapsed {
		parent, child := identities[pid], identities[childPID]
		if parent.target != "" && child.pkg == "" {
			target := "npm:" + packageWithoutVersion(parent.target)
			if old, ok := binAliases[child.key]; ok && old != target {
				binAliases[child.key] = ""
			} else if !ok {
				binAliases[child.key] = target
			}
			child.key = target
			identities[childPID] = child
		}
	}
	for pid, d := range identities {
		if target := binAliases[d.key]; target != "" && d.pkg == "" {
			d.key = target
			identities[pid] = d
		}
	}
	type key struct {
		host    int32
		command string
	}
	groups := map[key]*MCPPileup{}
	members := map[key][]model.ProcessIdentity{}
	for _, p := range procs {
		if p.Zombie || p.PID <= 0 {
			continue
		}
		if _, ok := hosts[p.PID]; ok {
			continue
		}
		if _, launcher := collapsed[p.PID]; launcher {
			continue
		}
		// Compare every descendant command, including servers whose entrypoint
		// has no MCP marker. Signature names are labels, never an allowlist.
		d := identities[p.PID]
		host := owner(p.PID)
		if host == 0 {
			continue
		}
		command := d.groupKey()
		if command == "" {
			continue
		}
		k := key{host, command}
		g := groups[k]
		if g == nil {
			g = &MCPPileup{MCPCommand: MCPCommand{Key: command, Name: d.name, HostCount: 1}, Host: hosts[host]}
			groups[k] = g
		}
		if d.name < g.Name {
			g.Name = d.name
		}
		addMCPProcess(&g.MCPCommand, p)
		members[k] = append(members[k], model.ProcessIdentity{PID: p.PID, StartedAt: p.StartTime})
		addMCPEvidence(&g.MCPCommand, p, d)
		cur := p
		for collapsed[cur.PPID] == cur.PID {
			launcher := byPID[cur.PPID]
			// Footprints include launchers, while Count counts logical servers.
			addMCPProcess(&g.MCPCommand, launcher)
			g.Count--
			g.IncludesLauncher = true
			addMCPEvidence(&g.MCPCommand, launcher, identities[launcher.PID])
			cur = launcher
		}
	}
	rollups := map[string]*MCPCommand{}
	for k, g := range groups {
		sort.Slice(members[k], func(i, j int) bool { return members[k][i].PID < members[k][j].PID })
		result.Samples = append(result.Samples, model.MCPObservation{At: now, Host: g.Host, Key: k.command, Name: g.Name, Count: g.Count, Footprint: g.Memory, Members: members[k]})
		finishMCPEvidence(&g.MCPCommand)
		g.DuplicateCount = max(0, g.Count-1)
		if g.Count > 1 {
			g.PileupCount = 1
			g.Advice = fmt.Sprintf("Restart %s (PID %d) host app/session; scope the MCP server per project.", g.Host.Name, g.Host.PID)
			result.Pileups = append(result.Pileups, *g)
		}
		r := rollups[k.command]
		if r == nil {
			r = &MCPCommand{Key: k.command, Name: g.Name}
			rollups[k.command] = r
		}
		if g.Name < r.Name {
			r.Name = g.Name
		}
		r.Commands = append(r.Commands, g.Commands...)
		r.Installs = append(r.Installs, g.Installs...)
		r.IncludesLauncher = r.IncludesLauncher || g.IncludesLauncher
		r.DuplicateCount += g.DuplicateCount
		r.Count += g.Count
		r.Memory += g.Memory
		r.HostCount++
		r.PileupCount += g.PileupCount
		mergeMeasure(r, g.MemoryMeasure)
		mergeStarts(r, g.OldestStart)
		mergeStarts(r, g.NewestStart)
	}
	for _, r := range rollups {
		finishMCPEvidence(r)
		result.Commands = append(result.Commands, *r)
	}
	less := func(a, b MCPCommand) bool {
		if a.Memory != b.Memory {
			return a.Memory > b.Memory
		}
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Command < b.Command
	}
	sort.Slice(result.Commands, func(i, j int) bool { return less(result.Commands[i], result.Commands[j]) })
	sort.Slice(result.Pileups, func(i, j int) bool {
		a, b := result.Pileups[i], result.Pileups[j]
		if a.Memory == b.Memory && a.Count == b.Count && a.Command == b.Command {
			return a.Host.PID < b.Host.PID
		}
		return less(a.MCPCommand, b.MCPCommand)
	})
	sort.Slice(result.Samples, func(i, j int) bool {
		a, b := result.Samples[i], result.Samples[j]
		if a.Host.PID != b.Host.PID {
			return a.Host.PID < b.Host.PID
		}
		return a.Key < b.Key
	})
	return result
}

func addMCPEvidence(g *MCPCommand, p model.Proc, d mcpIdentity) {
	command := p.Cmdline
	if command == "" {
		command = p.Exe
	}
	g.Commands = append(g.Commands, command)
	if d.target == "" || path.Base(d.entry) != "exec" {
		g.Installs = append(g.Installs, d.entry)
	}
	if p.Exe != "" {
		g.Installs = append(g.Installs, p.Exe)
	}
	if d.runtime != "" {
		g.Installs = append(g.Installs, d.runtime)
	}
}

func finishMCPEvidence(g *MCPCommand) {
	unique := func(values []string) []string {
		sort.Strings(values)
		out := values[:0]
		for _, v := range values {
			if v != "" && (len(out) == 0 || out[len(out)-1] != v) {
				out = append(out, v)
			}
		}
		return out
	}
	g.Commands, g.Installs = unique(g.Commands), unique(g.Installs)
	if len(g.Commands) > 0 {
		g.Command = g.Commands[0]
	}
}

func addMCPProcess(g *MCPCommand, p model.Proc) {
	g.Count++
	memory, measure := ProcessMemory(p)
	g.Memory += memory
	mergeMeasure(g, measure)
	mergeStarts(g, p.StartTime)
}
func mergeMeasure(g *MCPCommand, measure string) {
	if g.MemoryMeasure == "" {
		g.MemoryMeasure = measure
	} else if g.MemoryMeasure != measure {
		g.MemoryMeasure = "mixed"
	}
}
func mergeStarts(g *MCPCommand, at time.Time) {
	if at.IsZero() {
		return
	}
	if g.OldestStart.IsZero() || at.Before(g.OldestStart) {
		g.OldestStart = at
	}
	if g.NewestStart.IsZero() || at.After(g.NewestStart) {
		g.NewestStart = at
	}
}

var tempPath = regexp.MustCompile(`(^|[=:])((?:/private)?/(?:tmp|var/tmp)/[^:]*|(?:/private)?/var/folders/[^/]+/[^/]+/T/[^:]*)`)
var endpointPort = regexp.MustCompile(`(://[^/[:space:]]+):[0-9]+`)
var localPort = regexp.MustCompile(`((?:localhost|127\.[0-9]+\.[0-9]+\.[0-9]+|\[::1\])):[0-9]+`)
var portOption = regexp.MustCompile(`(?i)^--?(?:[a-z_-]+[-_])?port$`)

func normalizedMCPCommand(p model.Proc) string {
	args := slices.Clone(p.Argv)
	if len(args) == 0 {
		if p.Cmdline != "" || p.Exe == "" {
			return ""
		}
		args = []string{p.Exe}
	}
	if p.Exe != "" {
		args[0] = p.Exe
	}
	for i, arg := range args {
		if i > 0 && portOption.MatchString(args[i-1]) {
			if _, err := strconv.ParseUint(arg, 10, 16); err == nil {
				args[i] = "<port>"
				continue
			}
		}
		if option, value, ok := strings.Cut(arg, "="); ok && portOption.MatchString(option) {
			if _, err := strconv.ParseUint(value, 10, 16); err == nil {
				arg = option + "=<port>"
			}
		}
		arg = endpointPort.ReplaceAllString(arg, "${1}:<port>")
		arg = localPort.ReplaceAllString(arg, "${1}:<port>")
		arg = tempPath.ReplaceAllStringFunc(arg, func(value string) string {
			parts := tempPath.FindStringSubmatch(value)
			prefix, value := parts[1], parts[2]
			// Preserve the entrypoint identity in volatile installation directories.
			if i == 0 {
				return prefix + "<tmp>/" + path.Base(value)
			}
			switch path.Ext(value) {
			case ".js", ".mjs", ".cjs", ".py", ".jar":
				return prefix + "<tmp>/" + path.Base(value)
			}
			return prefix + "<tmp>"
		})
		args[i] = arg
	}
	for i, arg := range args {
		if strings.ContainsAny(arg, " \t\n\"'\\") {
			args[i] = strconv.Quote(arg)
		}
	}
	return strings.Join(args, " ")
}
