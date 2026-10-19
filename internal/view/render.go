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

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
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
		e.Schema = 1
		e.Data = v
		if e.Freshness == nil {
			e.Freshness = map[string]Fresh{}
		}
		e.CoverageGaps = nonNil(e.CoverageGaps)
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(e)
	}
	// Buffer table output so footer and formatting errors reach the caller.
	var buf strings.Builder
	_, noColor := os.LookupEnv("NO_COLOR")
	color := !opts.NoColor && !noColor && IsTerminal(w)
	if opts.Envelope.Mode == "snapshot" {
		fmt.Fprintln(&buf, "snapshot · no history")
	}
	tw := tabwriter.NewWriter(&buf, 0, 4, 2, ' ', 0)
	label := func(s string) string {
		if !color {
			return cell(s)
		}
		return StateStyle(s, false).Render(cell(s))
	}
	switch x := v.(type) {
	case ActivityView:
		fmt.Fprintln(tw, "MACHINE\tWINDOW\tSAMPLES\tEVENTS\tLATEST LOAD")
		for _, m := range x.Machines {
			samples, events, load := 0, 0, "—"
			for _, b := range m.Buckets {
				events += b.Events
				if b.Sample != nil {
					samples++
					load = fmt.Sprintf("%.1f", b.Sample.Load1)
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
		fmt.Fprintf(tw, "%d agents (%d working, %d waiting, %d stalled?) · inbox %d · %s · orphans %d ~%s · %s\n", x.Agents, x.Working, x.Waiting, x.Stalled, x.Inbox, HealthText(x.Sample, "%.0f"), x.Orphans, humanBytes(x.Reclaim), opts.Envelope.Mode)
	case AgentsView:
		renderAgents(tw, x, label)
	case AgentView:
		s := x.Session
		fmt.Fprintf(tw, "Machine:\t%s\n", cell(originLabel(x.MachineID, x.MachineFresh)))
		fmt.Fprintln(tw, "ID\tAGENT\tSTATE\tPROJECT / CWD\tBRANCH\tTITLE")
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", s.ID, cell(s.Adapter), label(x.Label), cell(s.Cwd), cell(s.Branch), cell(s.Title))
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
			fmt.Fprintf(tw, "%d\t%d\t%s\t%s\n", p.PID, p.PPID, humanBytes(p.RSS), cell(p.Cmdline))
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
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", cell(i.ID), label(i.Kind), age(i.Age), cell(i.Project), cell(i.Summary))
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
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t~%s\t%s\t%t\t%t\t%s\n", cell(o.ID), age(o.Age), cell(o.Signature), o.ProcCount, humanBytes(o.RSSTotal), cell(o.Cwd), o.Young, o.Verified, cell(strings.Join(o.Evidence, "; ")))
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
		ids := []string{}
		for _, h := range hosts {
			ids = append(ids, h.MachineID)
		}
		multi := multipleMachines(ids)
		fmt.Fprint(tw, machineHeading(multi))
		fmt.Fprintln(tw, "MEMORY\tCOMPRESSED\tSWAP\tLOAD (1/5/15)\tCORES\tDISK FREE\tPRESSURE FREE")
		for _, h := range hosts {
			s := h.Sample
			fmt.Fprint(tw, machinePrefix(multi, h.MachineID, h.MachineFresh))
			fmt.Fprintf(tw, "%s/%s\t%s\t%s/%s\t%.1f/%.1f/%.1f\t%d\t%s\t%.1f\n", humanBytes(s.MemUsed), humanBytes(s.MemTotal), humanBytes(s.MemCompressed), humanBytes(s.SwapUsed), humanBytes(s.SwapTotal), s.Load1, s.Load5, s.Load15, s.Cores, humanBytes(s.DiskFree), s.PressureFree)
		}
		fmt.Fprint(tw, machineHeading(multi))
		fmt.Fprintln(tw, "AGENT\tROLE\tMEM")
		for _, m := range x.Memory {
			fmt.Fprint(tw, machinePrefix(multi, m.MachineID, m.MachineFresh))
			fmt.Fprintf(tw, "%s\t%s\t%s\n", cell(m.Agent), cell(m.Role), humanBytes(m.RSS))
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
		if f.Stale || f.Status == "last-event" || f.Status == "not-yet-receiving" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		f := opts.Envelope.Freshness[name]
		line := fmt.Sprintf("stale: %s (%s)", cell(name), age(time.Duration(f.AgeSeconds)*time.Second))
		if !f.Stale {
			if f.Status == "last-event" {
				line = fmt.Sprintf("last event: %s %s (%s ago)", cell(name), f.LastOKAt.Format(time.RFC3339), age(time.Duration(f.AgeSeconds)*time.Second))
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
			title := cell(r.Title)
			if r.FinishedChildren > 0 {
				title += fmt.Sprintf(" (%d workers finished)", r.FinishedChildren)
			}
			fmt.Fprint(w, machinePrefix(multi, r.MachineID, r.MachineFresh))
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.ID, name, label(r.Label), cell(r.Origin), cell(r.Project), cell(r.Branch), sessionAgeText(r.Age), humanBytes(r.Mem), title, cell(r.Evidence))
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
		fmt.Fprintf(w, "%s\t%s\t%v\t%t\n", cell(c.Kind), cell(c.Path), c.SessionIDs, c.Family)
	}
}

func humanBytes(n uint64) string {
	if n >= 1<<30 {
		return fmt.Sprintf("%.1fG", float64(n)/(1<<30))
	}
	return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
}
func age(d time.Duration) string { return max(0, d).Round(time.Second).String() }

// sessionAgeText renders an unknown (zero) session age as "—".
func sessionAgeText(d time.Duration) string {
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
	return fmt.Sprintf("swap %s/%s · load "+loadFormat+"/%d", humanBytes(sample.SwapUsed), humanBytes(sample.SwapTotal), sample.Load1, sample.Cores)
}
