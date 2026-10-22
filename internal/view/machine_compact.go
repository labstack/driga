package view

// compactMachine bounds evidence lists after calculating whole-machine totals.
// Full reports retain all evidence; truncation never changes memory/CPU totals.
func compactMachine(v *MachineView) {
	bound := func(length, limit int) int {
		if length > limit {
			v.Truncated = true
		}
		return min(length, limit)
	}
	const rows, detail = 10, 3
	v.SessionCosts = v.SessionCosts[:bound(len(v.SessionCosts), rows)]
	for i := range v.SessionCosts {
		ps := v.SessionCosts[i].Processes
		v.SessionCosts[i].Processes = ps[:bound(len(ps), detail)]
	}
	apps := func(apps []MachineApp) []MachineApp {
		apps = apps[:bound(len(apps), rows)]
		for i := range apps {
			ps := apps[i].Processes
			apps[i].Processes = ps[:bound(len(ps), detail)]
		}
		return apps
	}
	v.AgentNoSession, v.LikelyAgent = apps(v.AgentNoSession), apps(v.LikelyAgent)
	v.TopMemory, v.TopCPU = apps(v.TopMemory), apps(v.TopCPU)
	mcp := &v.MCPServers
	mcp.Trends = mcp.Trends[:bound(len(mcp.Trends), rows)]
	mcp.Commands = mcp.Commands[:bound(len(mcp.Commands), rows)]
	mcp.Pileups = mcp.Pileups[:bound(len(mcp.Pileups), rows)]
	for i := range mcp.Commands {
		c := &mcp.Commands[i]
		c.Trends = c.Trends[:bound(len(c.Trends), detail)]
		c.Commands = c.Commands[:bound(len(c.Commands), detail)]
		c.Installs = c.Installs[:bound(len(c.Installs), detail)]
	}
	for i := range mcp.Pileups {
		c := &mcp.Pileups[i]
		c.Commands = c.Commands[:bound(len(c.Commands), detail)]
		c.Installs = c.Installs[:bound(len(c.Installs), detail)]
	}
}
