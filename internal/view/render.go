package view

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"

	"github.com/labstack/driga/internal/textfmt"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
)

// RenderOpts controls JSON, colors, and the common metadata around tables.
type RenderOpts struct {
	JSON     bool
	NoColor  bool
	Envelope Envelope
}

// IsTerminal reports whether the output writer is an interactive terminal.
func IsTerminal(w io.Writer) bool { f, ok := w.(*os.File); return ok && term.IsTerminal(f.Fd()) }

// Render writes a typed table or a stable versioned JSON envelope.
func Render(w io.Writer, v any, opts RenderOpts) error {
	if opts.JSON {
		e := opts.Envelope
		e.Schema = 2
		e.Data = v
		if e.Freshness == nil {
			e.Freshness = map[string]Fresh{}
		}
		e.CoverageGaps = nonNil(e.CoverageGaps)
		enc := json.NewEncoder(w)

		return enc.Encode(e)
	}
	// Buffer table output so footer and formatting errors reach the caller.
	var buf strings.Builder
	noColor := os.Getenv("NO_COLOR") != ""
	color := !opts.NoColor && !noColor && IsTerminal(w)
	if opts.Envelope.Mode == "snapshot" {
		fmt.Fprintln(&buf, "snapshot · no history")
	}
	tw := tabwriter.NewWriter(&buf, 0, 4, 2, ' ', 0)
	label := func(s string) string {
		s = FriendlyEnum(s)
		if !color {
			return cell(s)
		}
		return StateStyle(s, false).Render(cell(s))
	}
	switch x := v.(type) {
	case FlowView:
		if x.Lead == nil {
			fmt.Fprintln(tw, "No live interactive session.")
			break
		}
		fmt.Fprintf(tw, "lead · %s · %s · %s\n", cell(x.Lead.Adapter), cell(x.Lead.DisplayTitle()), label(derive.Display(x.Lead.Label, x.Lead.Heuristic)))
		fmt.Fprintf(tw, "project %s · %s\n", cell(x.Lead.Project), cell(x.Lead.Branch))
		for _, r := range x.Mixer.Rows {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%.2f\t%t\n", r.Name, cell(r.Reading), age(r.Age), r.Strength, r.Supports)
		}
		fmt.Fprintf(tw, "verdict %s · %d of 4 agree\n", x.Mixer.Verdict, x.Mixer.Agreement)
		for _, c := range x.Children {
			fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%s\n", c.Kind, c.ID, cell(c.DisplayTitle()), label(derive.Display(c.Label, c.Heuristic)), FlowSpark(c.Activity), age(c.Elapsed))
		}
	case ActivityView:
		fmt.Fprintln(tw, "MACHINE\tWINDOW\tSAMPLES\tEVENTS\tLATEST LOAD")
		for _, m := range x.Machines {
			samples, events, load := 0, 0, "—"
			for _, b := range m.Buckets {
				events += b.Events
				if b.Sample != nil {
					samples++
					load = FormatLoad(b.Sample.Load1, "%.1f")
				}
			}
			fmt.Fprintf(tw, "%s\t60m\t%d/60\t%d\t%s\n", cell(originLabel(m.MachineID, m.MachineFresh)), samples, events, load)
		}
	case MachinesView:
		fmt.Fprintln(tw, "NAME\tID\tHOST\tOS\tSTATE\tLAST SEEN\tAGENTS\tINBOX\tSKEW\tLAST ERROR")
		for _, m := range x.Rows {
			seen := "never"
			if !lastSeen(m.Machine).IsZero() {
				seen = age(m.LastSeenAge) + " ago"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\t%s\t%s\n", cell(MachineName(m.Machine)), cell(m.MachineID), cell(m.Hostname), cell(m.OS), cell(m.State), seen, m.Agents, m.Inbox, cell(m.SkewWarning), cell(m.LastError))
		}
		if x.Warning != "" {
			fmt.Fprintln(tw, cell(x.Warning))
		}
	case SummaryView:
		fmt.Fprintf(tw, "%d agents (%d working, %d waiting, %d stuck) · inbox %d · %s · orphans %d ~%s · %s\n", x.Agents, x.Working, x.Waiting, x.Stuck, x.Inbox, HealthText(x.Sample, "%.0f"), x.Orphans, HumanBytes(x.Reclaim), opts.Envelope.Mode)
	case AgentsView:
		renderAgents(tw, x, label)
	case AgentView:
		s := x.Session
		fmt.Fprintf(tw, "Machine:\t%s\n", cell(originLabel(x.MachineID, x.MachineFresh)))
		fmt.Fprintln(tw, "ID\tAGENT\tSTATE\tPROJECT / CWD\tBRANCH\tTITLE")
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", s.ID, cell(s.Adapter), label(x.Label), cell(s.Cwd), cell(s.Branch), cell(s.Title+FromSuffix(s.StartedFrom)))
		fmt.Fprintf(tw, "Evidence:\t%s\nObserved at:\t%s\n", cell(s.Evidence), x.ObservedAt.Format(time.RFC3339))
		if s.LastPrompt != "" {
			fmt.Fprintf(tw, "Last prompt:\t%s\n", cell(s.LastPrompt))
		}
		if x.Resume != "" {
			fmt.Fprintf(tw, "Resume:\t%s\n", cell(x.Resume))
		}
		fmt.Fprintln(tw, "FILES\tLAST EDIT")
		for _, f := range x.Files {
			fmt.Fprintf(tw, "%s\t%s\n", cell(f.Path), f.TS.Format(time.RFC3339))
		}
		fmt.Fprintln(tw, "PID\tPPID\tMEM\tCOMMAND")
		for _, p := range x.Procs {
			fmt.Fprintf(tw, "%d\t%d\t%s\t%s\n", p.PID, p.PPID, HumanBytes(p.RSS), cell(p.Cmdline))
		}
		fmt.Fprintln(tw, "EVENT\tAT\tSUMMARY")
		for _, e := range x.Events {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", cell(e.Kind), e.TS.Format(time.RFC3339), cell(e.Summary))
		}
	case ProjectsView:
		renderProjects(tw, x, opts.Envelope.ObservedAt)
	case WhereView:
		renderProjects(tw, ProjectsView{Rows: []ProjectRow{x.ProjectRow}}, opts.Envelope.ObservedAt)
		for _, risk := range x.Status.AtRisk {
			fmt.Fprintln(tw, "At risk:", cell(risk))
		}
		for _, part := range x.Status.Where {
			fmt.Fprintf(tw, "%s\t%s (%s)\n", cell(part.Source), cell(part.Text), age(opts.Envelope.ObservedAt.Sub(part.At)))
		}
		fmt.Fprintln(tw, "WORKTREE\tBRANCH\tDIRTY\tUNPUSHED")
		for _, wt := range x.WorktreeRows {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%d\n", cell(wt.Path), cell(wt.Branch), wt.Dirty+wt.Untracked, wt.Ahead)
		}
		renderAgents(tw, x.Agents, label)
		fmt.Fprintln(tw, "HANDOFF\tAT")
		for _, h := range x.Handoffs {
			fmt.Fprintf(tw, "%s\t%s\n", cell(h.Text), h.TS.Format(time.RFC3339))
		}
	case InboxView:
		ids := []string{}
		stale := false
		for _, r := range x.Rows {
			ids = append(ids, r.MachineID)
			stale = stale || r.Stale
		}
		multi := multipleMachines(ids) || stale
		fmt.Fprint(tw, machineHeading(multi))
		fmt.Fprintln(tw, "ID\tKIND\tAGE\tPROJECT\tSUMMARY")
		for _, i := range x.Rows {
			fmt.Fprint(tw, machinePrefix(multi, i.MachineID, i.MachineFresh))
			kind := i.Kind
			if i.Historical && kind == "waiting" {
				kind = "historical wait"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", cell(i.ID), label(kind), age(i.Age), cell(i.Project), cell(i.Summary))
		}
	case ConflictsView:
		fmt.Fprintln(tw, "current")
		renderConflicts(tw, x.Current)
		fmt.Fprintln(tw, "recent overlap (last 6h)")
		renderConflicts(tw, x.Recent)
	case OrphansView:
		ids := []string{}
		stale := false
		for _, r := range x.Rows {
			ids = append(ids, r.MachineID)
			stale = stale || r.Stale
		}
		multi := multipleMachines(ids) || stale
		fmt.Fprint(tw, machineHeading(multi))
		fmt.Fprintln(tw, "ID\tAGE\tSIGNATURE\tPROCS\t~RECLAIM\tCWD\tYOUNG\tVERIFIED\tEVIDENCE")
		for _, o := range x.Rows {
			fmt.Fprint(tw, machinePrefix(multi, o.MachineID, o.MachineFresh))
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t~%s\t%s\t%t\t%t\t%s\n", cell(o.ID), age(o.Age), cell(o.Signature), o.ProcCount, HumanBytes(o.RSSTotal), cell(o.Cwd), o.Young, o.Verified, cell(strings.Join(o.Evidence, "; ")))
		}
		if opts.Envelope.Mode == "snapshot" {
			fmt.Fprintln(tw, "unverified (snapshot)")
		}
		fmt.Fprintln(tw, "driga orphans kill <id>")
	case MachineView:
		hosts := x.Machines
		if len(hosts) == 0 {
			hosts = []MachineView{x}
		}
		for _, h := range hosts {
			if len(hosts) > 1 {
				fmt.Fprintln(tw, "Machine ·", cell(h.MachineName))
			}
			fmt.Fprintln(tw, cell(MachineSummary(h)))
			fmt.Fprintf(tw, "Memory measure: %s · %s\n", h.AgentShare.MemoryMeasure, FootprintNote)
			apps := func(rows []MachineApp, processes bool) {
				for _, a := range rows {
					fmt.Fprintf(tw, "%s\t%s\t%s\t%.1f%% CPU\n", cell(a.Name), HumanBytes(a.Memory), a.MemoryMeasure, a.CPUPercent)
					if processes {
						for _, p := range a.Processes {
							fmt.Fprintf(tw, "  PID %d %s\t%s\t%.1f%% CPU\t%s\n", p.PID, cell(p.Exe), HumanBytes(p.Memory), p.CPUPercent, cell(AttributionText(p.Attribution)))
						}
					}
				}
			}
			fmt.Fprintln(tw, "Sessions by cost")
			apps(MachineSessionApps(h), true)
			fmt.Fprintln(tw, "Agent processes without a session")
			apps(h.AgentNoSession, false)
			fmt.Fprintln(tw, "MCP servers")
			for _, c := range h.MCPServers.Commands {
				fmt.Fprintln(tw, cell(MCPCommandSummary(c)))
			}
			if len(h.MCPServers.Pileups) == 0 {
				fmt.Fprintln(tw, "No MCP pile-ups.")
			}
			for _, p := range h.MCPServers.Pileups {
				if !h.Full {
					fmt.Fprintf(tw, "%s: %d processes · %s · Host: %s (PID %d) · age %s · Oldest: %s · Newest: %s\n%s\n", cell(p.Name), p.Count, HumanBytes(p.Memory), cell(p.Host.Name), p.Host.PID, age(p.Host.Age), p.OldestStart.Format(time.RFC3339), p.NewestStart.Format(time.RFC3339), cell(p.Advice))
					continue
				}
				for _, line := range strings.Split(MCPPileupText(p), "\n") {
					fmt.Fprintln(tw, cell(line))
				}
			}
			if len(h.LikelyAgent) > 0 {
				fmt.Fprintln(tw, "Likely agent work (shared with editor)")
				apps(h.LikelyAgent, false)
			}
			other := MachineOtherApps(h)
			fmt.Fprintf(tw, "Other apps · %s (%s) · %.1f%% CPU\n", HumanBytes(other.Memory), other.MemoryMeasure, other.CPUPercent)
			for _, rank := range []struct {
				name string
				apps []MachineApp
			}{{"Top 10 apps by memory", h.TopMemory}, {"Top 10 apps by CPU", h.TopCPU}} {
				fmt.Fprintln(tw, "  "+rank.name)
				apps(rank.apps, false)
			}
			s := h.Sample
			fmt.Fprintln(tw, "Vitals detail")
			fmt.Fprintln(tw, "MEMORY\tCOMPRESSED\tSWAP\tLOAD (1/5/15)\tCORES\tDISK FREE\tPRESSURE LEVEL / FREE")
			fmt.Fprintf(tw, "%s/%s\t%s\t%s/%s\t%s/%s/%s\t%d\t%s\t%s / %.1f\n", HumanBytes(s.MemUsed), HumanBytes(s.MemTotal), HumanBytes(s.MemCompressed), HumanBytes(s.SwapUsed), HumanBytes(s.SwapTotal), FormatLoad(s.Load1, "%.1f"), FormatLoad(s.Load5, "%.1f"), FormatLoad(s.Load15, "%.1f"), s.Cores, HumanBytes(s.DiskFree), s.PressureLevel, s.PressureFree)
			if h.Full {
				fmt.Fprintln(tw, "Retained MCP trends · minute detail for 24h; hourly endpoints to 30 days")
				for _, t := range h.MCPServers.Trends {
					fmt.Fprintf(tw, "%s · host PID %d · %s · started %d / ended %d · %s to %s\n", cell(t.Name), t.Host.PID, cell(t.State), t.StartedCount, t.EndedCount, t.FirstSample.Format(time.RFC3339), t.LastSample.Format(time.RFC3339))
				}
			} else {
				fmt.Fprintln(tw, "MCP trends: last 24h · driga machines show --full for retained history and process detail")
				if h.Truncated {
					fmt.Fprintln(tw, "Showing top 10 rows and up to 3 evidence entries per row; totals include all sampled processes.")
				}
			}
			fmt.Fprintln(tw, "Agent memory by role")
			fmt.Fprintln(tw, "AGENT\tROLE\tMEM")
			for _, r := range h.Memory {
				fmt.Fprintf(tw, "%s\t%s\t%s\n", cell(r.Agent), cell(r.Role), HumanBytes(r.RSS))
			}
		}

	default:
		return fmt.Errorf("unsupported view %T", v)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if !opts.Envelope.ObservedAt.IsZero() {
		fmt.Fprintln(&buf, "observed_at:", opts.Envelope.ObservedAt.Format(time.RFC3339))
	}
	names := []string{}
	for name, f := range opts.Envelope.Freshness {
		if f.Stale || f.Status == "last_event" || f.Status == "not_yet_receiving" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		f := opts.Envelope.Freshness[name]
		line := fmt.Sprintf("stale: %s (%s)", cell(name), age(time.Duration(f.AgeMS)*time.Millisecond))
		if !f.Stale {
			if f.Status == "last_event" {
				line = fmt.Sprintf("last event: %s %s (%s ago)", cell(name), f.LastOKAt.Format(time.RFC3339), age(time.Duration(f.AgeMS)*time.Millisecond))
			} else {
				line = fmt.Sprintf("%s: not-yet-receiving", cell(name))
			}
		}
		if color {
			line = lipgloss.NewStyle().Faint(true).Render(line)
		}
		fmt.Fprintln(&buf, line)
	}
	for _, g := range opts.Envelope.CoverageGaps {
		fmt.Fprintf(&buf, "coverage gap: %s – %s\n", g.From.Format(time.RFC3339), g.To.Format(time.RFC3339))
	}
	_, err := io.WriteString(w, buf.String())
	return err
}

func renderAgents(w io.Writer, v AgentsView, label func(string) string) {
	ids := []string{}
	stale := false
	var collect func([]AgentRow)
	collect = func(rows []AgentRow) {
		for _, r := range rows {
			ids = append(ids, r.MachineID)
			stale = stale || r.Stale
			collect(r.Children)
		}
	}
	collect(v.Rows)
	multi := multipleMachines(ids) || stale
	fmt.Fprint(w, machineHeading(multi))
	fmt.Fprintln(w, "ID\tAGENT\tSTATE\tORIGIN\tPROJECT\tBRANCH\tAGE\tMEM\tTITLE\tEVIDENCE")
	var rows func([]AgentRow, int)
	rows = func(rs []AgentRow, depth int) {
		for _, r := range rs {
			name := cell(r.Adapter)
			if depth > 0 {
				name = strings.Repeat("  ", depth-1) + "↳ " + name
			}
			title := cell(r.Title + FromSuffix(r.StartedFrom))
			if r.FinishedChildren > 0 {
				title += fmt.Sprintf(" (%d workers finished)", r.FinishedChildren)
			}
			fmt.Fprint(w, machinePrefix(multi, r.MachineID, r.MachineFresh))
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.ID, name, label(r.Label), cell(r.Origin), cell(r.Project), cell(r.Branch), SessionDuration(r.Age), HumanBytes(r.Mem), title, cell(r.Evidence))
			rows(r.Children, depth+1)
		}
	}
	rows(v.Rows, 0)
}

func renderProjects(w io.Writer, v ProjectsView, now time.Time) {
	ids := []string{}
	stale := false
	for _, r := range v.Rows {
		ids = append(ids, r.MachineID)
		stale = stale || r.Stale
	}
	multi := multipleMachines(ids) || stale
	fmt.Fprint(w, machineHeading(multi))
	fmt.Fprintln(w, "PROJECT\tACTIVITY\tAGENTS\tDIRTY\tUNPUSHED\tWORKTREES\tWHERE")
	for _, r := range v.Rows {
		where := ""
		if len(r.Status.Where) > 0 {
			p := r.Status.Where[0]
			where = fmt.Sprintf("%s (%s, %s)", cell(p.Text), cell(p.Source), age(now.Sub(p.At)))
			if p.Source == "claude" {
				where = fmt.Sprintf("%s · claude %s", cell(p.Text), age(now.Sub(p.At)))
			}
			if p.Stale {
				where += " (stale)"
			}
		}
		fmt.Fprint(w, machinePrefix(multi, r.MachineID, r.MachineFresh))
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%d\t%d\t%s\n", cell(r.Status.Repo.Name), r.Status.Activity, r.Status.LiveAgents, r.Dirty, r.Unpushed, r.Worktrees, where)
	}
}

func renderConflicts(w io.Writer, rs []ConflictRow) {
	ids := []string{}
	stale := false
	for _, r := range rs {
		ids = append(ids, r.MachineID)
		stale = stale || r.Stale
	}
	multi := multipleMachines(ids) || stale
	fmt.Fprint(w, machineHeading(multi))
	fmt.Fprintln(w, "KIND\tPATH\tSESSIONS\tFAMILY")
	for _, c := range rs {
		fmt.Fprint(w, machinePrefix(multi, c.MachineID, c.MachineFresh))
		fmt.Fprintf(w, "%s\t%s\t%v\t%t\n", cell(FriendlyEnum(c.Kind)), cell(c.Path), c.SessionIDs, c.Family)
	}
}

// HumanBytes formats compact terminal memory values.
func HumanBytes(n uint64) string { return textfmt.Bytes(float64(n), true) }

func age(d time.Duration) string { return max(0, d).Round(time.Second).String() }

// SessionDuration renders a session duration, or "—" for an unknown (zero) age.
func SessionDuration(d time.Duration) string {
	if d == 0 {
		return "—"
	}
	return age(d)
}

// cell keeps stored control sequences and multiline text out of table layout.
func cell(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

// FootprintNote explains why app memory can add up to more than RAM.
const FootprintNote = "footprint includes compressed and swapped memory, so apps can add up to more than RAM"

func multipleMachines(ids []string) bool {
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	return len(seen) > 1
}
func machineHeading(show bool) string {
	if show {
		return "MACHINE\t"
	}
	return ""
}
func originLabel(id string, f MachineFresh) string {
	name := f.MachineName
	if name == "" {
		name = id
	}
	if f.Stale {
		name += " (" + f.State + " " + age(f.LastSeenAge) + ")"
	}
	return name
}
func machinePrefix(show bool, id string, f MachineFresh) string {
	if show {
		return cell(originLabel(id, f)) + "\t"
	}
	return ""
}

// HealthText formats swap and load, or says health is unavailable when no
// machine sample exists, rather than showing zeros as if measured.
func HealthText(sample model.MachineSample, loadFormat string) string {
	if sample.TS.IsZero() {
		return "health unavailable"
	}
	return fmt.Sprintf("swap %s/%s · load %s/%d", HumanBytes(sample.SwapUsed), HumanBytes(sample.SwapTotal), FormatLoad(sample.Load1, loadFormat), sample.Cores)
}

// FromSuffix is the shared short launch-surface wording for session surfaces.
func FromSuffix(app string) string {
	if app == "" {
		return ""
	}
	return " · from " + app
}
