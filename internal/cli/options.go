package cli

// Command options are declared on the leaves that consume them.
type ReadOptions struct {
	Machine  string `default:"all" help:"Scope to a machine name, ID, device-ID prefix, local, or all."`
	Project  string `help:"Scope to one project (path, name, or dot)."`
	Since    string `help:"Only show items since a duration ago or yesterday."`
	Snapshot bool   `help:"Force one-shot snapshot collection without history."`
	JSON     bool   `help:"Print versioned JSON instead of tables."`
	NoColor  bool   `name:"no-color" help:"Disable colors."`
}

func (o *ReadOptions) AfterApply(g *Globals) error {
	g.Machine = o.Machine
	g.Project = o.Project
	g.Since = o.Since
	g.Snapshot = o.Snapshot
	g.JSON = o.JSON
	g.NoColor = o.NoColor
	return nil
}

type DetailOptions struct {
	Machine  string `default:"all" help:"Scope to a machine name, ID, device-ID prefix, local, or all."`
	Project  string `help:"Scope to one project (path, name, or dot)."`
	Snapshot bool   `help:"Force one-shot snapshot collection without history."`
	JSON     bool   `help:"Print versioned JSON instead of tables."`
	NoColor  bool   `name:"no-color" help:"Disable colors."`
}

func (o *DetailOptions) AfterApply(g *Globals) error {
	g.Machine = o.Machine
	g.Project = o.Project
	g.Snapshot = o.Snapshot
	g.JSON = o.JSON
	g.NoColor = o.NoColor
	return nil
}

type ProjectDetailOptions struct {
	Machine  string `default:"all" help:"Scope to a machine name, ID, device-ID prefix, local, or all."`
	Snapshot bool   `help:"Force one-shot snapshot collection without history."`
	JSON     bool   `help:"Print versioned JSON instead of tables."`
	NoColor  bool   `name:"no-color" help:"Disable colors."`
}

func (o *ProjectDetailOptions) AfterApply(g *Globals) error {
	g.Machine = o.Machine
	g.Snapshot = o.Snapshot
	g.JSON = o.JSON
	g.NoColor = o.NoColor
	return nil
}

type OrphanOptions struct {
	Project  string `help:"Scope candidates to one project (path, name, or dot)."`
	Machine  string `default:"all" help:"Scope to a machine name, ID, device-ID prefix, local, or all."`
	Since    string `help:"Only show items since a duration ago or yesterday."`
	Snapshot bool   `help:"Force one-shot snapshot collection without history."`
	JSON     bool   `help:"Print versioned JSON instead of tables."`
	NoColor  bool   `name:"no-color" help:"Disable colors."`
}

func (o *OrphanOptions) AfterApply(g *Globals) error {
	g.Machine = o.Machine
	g.Project = o.Project
	g.Since = o.Since
	g.Snapshot = o.Snapshot
	g.JSON = o.JSON
	g.NoColor = o.NoColor
	return nil
}

type MachineDetailOptions struct {
	Project  string `help:"Scope process detail to one project (path, name, or dot)."`
	Snapshot bool   `help:"Force one-shot snapshot collection without history."`
	JSON     bool   `help:"Print versioned JSON instead of tables."`
	NoColor  bool   `name:"no-color" help:"Disable colors."`
}

func (o *MachineDetailOptions) AfterApply(g *Globals) error {
	g.Project = o.Project
	g.Snapshot = o.Snapshot
	g.JSON = o.JSON
	g.NoColor = o.NoColor
	return nil
}

type MachineListOptions struct {
	Machine  string `default:"all" help:"Scope to a machine name, ID, device-ID prefix, local, or all."`
	Snapshot bool   `help:"Force one-shot snapshot collection without history."`
	JSON     bool   `help:"Print versioned JSON instead of tables."`
	NoColor  bool   `name:"no-color" help:"Disable colors."`
}

func (o *MachineListOptions) AfterApply(g *Globals) error {
	g.Machine = o.Machine
	g.Snapshot = o.Snapshot
	g.JSON = o.JSON
	g.NoColor = o.NoColor
	return nil
}

type TopOptions struct {
	Machine  string  `default:"all" help:"Scope to a machine name, ID, device-ID prefix, local, or all."`
	Project  string  `help:"Scope to one project (path, name, or dot)."`
	Since    string  `help:"Only show items since a duration ago or yesterday."`
	Snapshot bool    `help:"Force one-shot snapshot collection without history."`
	NoColor  bool    `name:"no-color" help:"Disable colors."`
	Theme    *string `enum:"system,light,dark" help:"TUI theme: system, light or dark (overrides ui.theme)."`
}

func (o *TopOptions) AfterApply(g *Globals) error {
	g.Machine = o.Machine
	g.Project = o.Project
	g.Since = o.Since
	g.Snapshot = o.Snapshot
	g.NoColor = o.NoColor
	g.Theme = o.Theme
	return nil
}

type WebOptions struct {
	Machine  string `default:"all" help:"Scope to a machine name, ID, device-ID prefix, local, or all."`
	Project  string `help:"Scope to one project (path, name, or dot)."`
	Since    string `help:"Only show items since a duration ago or yesterday."`
	Snapshot bool   `help:"Force one-shot snapshot collection without history."`
}

func (o *WebOptions) AfterApply(g *Globals) error {
	g.Machine = o.Machine
	g.Project = o.Project
	g.Since = o.Since
	g.Snapshot = o.Snapshot
	return nil
}

type AskOptions struct {
	Machine string `default:"all" help:"Scope to a machine name, ID, device-ID prefix, local, or all."`
	Project string `help:"Scope to one project (path, name, or dot)."`
	JSON    bool   `help:"Print versioned JSON instead of tables."`
}

func (o *AskOptions) AfterApply(g *Globals) error {
	g.Machine = o.Machine
	g.Project = o.Project
	g.JSON = o.JSON
	return nil
}

type JSONOptions struct {
	JSON bool `help:"Print versioned JSON instead of tables."`
}

func (o *JSONOptions) AfterApply(g *Globals) error {
	g.JSON = o.JSON
	return nil
}

type MachineOptions struct {
	Machine string `default:"all" help:"Scope to a machine name, ID, device-ID prefix, local, or all."`
}

func (o *MachineOptions) AfterApply(g *Globals) error {
	g.Machine = o.Machine
	return nil
}

type OrphanActionOptions struct {
	Machine  string `default:"all" help:"Select orphan candidates from this machine, local, or all."`
	Snapshot bool   `help:"Collect one snapshot before selecting candidates."`
}

func (o *OrphanActionOptions) AfterApply(g *Globals) error {
	g.Machine = o.Machine
	g.Snapshot = o.Snapshot
	return nil
}
