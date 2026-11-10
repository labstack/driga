package reclaim

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
	"github.com/labstack/driga/internal/view"
)

// ProcessReviewInput is one current native snapshot. Missing map entries mean
// observations are unavailable, even when a process's numeric CPU is zero.
type ProcessReviewInput struct {
	Processes []model.Proc
	Sessions  []model.Session
	Orphans   []view.OrphanRow
	SourceOS  string
	Now       time.Time
	MinAge    time.Duration
	TargetPID int32
	Self      model.Proc
	Peers     map[int32]model.PeerState
	CPUKnown  map[int32]bool
}

// ProcessReview explains a tree for human review; it never authorizes stopping.
type ProcessReview struct {
	ID             string
	RootPID        int32
	PPID           int32
	StartTime      time.Time
	Cwd            string
	Name           string
	Category       string
	Status         string
	Reasons        []string
	Age            time.Duration
	PIDs           []int32
	CPU            float64
	CPUKnown       bool
	Memory         derive.MemoryUsage
	OwnershipKnown bool
	IdentityKnown  bool
	PeersKnown     bool
}

// ReviewProcesses is read-only: all topology, ownership and resource evidence is
// supplied by the caller. An absent explicit PID returns no record.
func ReviewProcesses(in ProcessReviewInput) []ProcessReview {
	idx, err := derive.NewProcIndexForOS(in.Processes, in.SourceOS)
	if err != nil {
		return nil
	}
	ordered := append([]model.Proc(nil), in.Processes...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].PID < ordered[j].PID })
	selfTree := map[int32]bool{in.Self.PID: true}
	for _, p := range idx.Descendants(in.Self.PID) {
		selfTree[p.PID] = true
	}
	for _, pid := range idx.Ancestors(in.Self.PID) {
		selfTree[pid] = true
	}
	rows := []ProcessReview{}
	classifier := derive.NewClassifier(signatures.Builtin(), len(ordered))
	type processLabel struct{ category, name string }
	labels := make(map[int32]processLabel, len(ordered))
	nativeAgents := derive.NativeAgentProcesses(ordered, in.Self, in.SourceOS, classifier)
	for _, p := range ordered {
		category, name := reviewCategory(p, classifier)
		labels[p.PID] = processLabel{category, name}
	}
	claimed := map[int32]bool{}
	liveSessions := map[int32]bool{}
	for _, s := range in.Sessions {
		p, ok := idx.Get(s.PID)
		if ok && s.EndedAt == nil && s.Label != "dead" && s.Label != "ended" && !s.ProcStart.IsZero() && derive.SameStart(p.StartTime, s.ProcStart) {
			liveSessions[p.PID] = true
		}
	}
	for _, root := range ordered {
		if in.TargetPID != 0 && root.PID != in.TargetPID {
			continue
		}
		category, name := labels[root.PID].category, labels[root.PID].name
		detached := reviewDetached(idx, root, in.SourceOS)
		age := time.Duration(0)
		if !root.StartTime.IsZero() && !in.Now.IsZero() {
			age = max(time.Duration(0), in.Now.Sub(root.StartTime))
		}
		owned, ownerKnown := derive.SameProcessOwner(root, in.Self, in.SourceOS)
		if in.TargetPID == 0 && (claimed[root.PID] || liveSessions[root.PID] || root.PID <= 1 || !detached || !owned || root.Zombie || category == "managed" || category == "database" || selfTree[root.PID] || !root.StartTime.IsZero() && age < in.MinAge) {
			continue
		}
		tree := append([]model.Proc{root}, idx.Descendants(root.PID)...)
		r := ProcessReview{ID: fmt.Sprintf("process:%d:%d", root.PID, root.StartTime.UnixNano()), RootPID: root.PID, PPID: root.PPID, StartTime: root.StartTime, Cwd: root.Cwd, Name: name, Category: category, Status: "review", Age: age, CPUKnown: true, PeersKnown: true, OwnershipKnown: ownerKnown, Memory: derive.SumProcessMemory(tree)}
		protect := func(reason string) { r.Status = "protected"; r.Reasons = append(r.Reasons, reason) }
		if detached {
			r.Reasons = append(r.Reasons, "detached parent relationship; review whether this tree is still needed")
		} else {
			protect("attached to a current parent process")
		}
		if root.PID <= 1 {
			protect("system PID is protected")
		}
		if root.StartTime.IsZero() {
			r.Reasons = append(r.Reasons, "start time unavailable; age and identity cannot be established")
		} else if age < in.MinAge {
			protect("too young for detached process review")
		}
		if category == "test_database" {
			r.Reasons = append(r.Reasons, "database has temporary data-directory provenance; confirm test workload has ended")
		}
		r.IdentityKnown = true
		for _, p := range tree {
			r.PIDs = append(r.PIDs, p.PID)
			claimed[p.PID] = true
			memberOwned, memberKnown := derive.SameProcessOwner(p, in.Self, in.SourceOS)
			r.OwnershipKnown = r.OwnershipKnown && memberKnown
			if p.Cwd == "" {
				r.OwnershipKnown = false
				protect("ownership requires review: working directory unavailable for a tree member")
			}
			if !memberKnown {
				protect("process owner unavailable")
			} else if !memberOwned {
				protect("process belongs to a different user")
			}
			if p.Partial || p.Exe == "" || len(p.Argv) == 0 || p.Cmdline == "" || p.StartTime.IsZero() {
				r.IdentityKnown = false
			}
			if p.Zombie {
				protect("zombie process; waiting for its parent to reap it")
			}
			cat := labels[p.PID].category
			if cat == "managed" || cat == "database" || selfTree[p.PID] {
				protect("managed application, service, database, or Driga process is protected")
			}
			for _, ancestor := range idx.Ancestors(p.PID) {
				// Init is the adopter of detached work, not its workload owner.
				if ancestor <= 1 {
					continue
				}
				c := labels[ancestor].category
				if c == "managed" || c == "database" {
					protect("tree belongs to a managed application or service")
					break
				}
			}
			if !in.CPUKnown[p.PID] {
				r.CPUKnown = false
			} else {
				r.CPU += p.CPUPercent
				if p.CPUPercent >= derive.BackgroundCPUPercent {
					protect("last observed CPU indicates activity in a tree member")
				}
			}
			state, ok := in.Peers[p.PID]
			if !ok {
				r.PeersKnown = false
			} else if derive.InUse(classifier, root, p, state) {
				if state == model.PeerListening {
					protect("listening socket may still serve a workload")
				} else {
					protect("connected socket may still serve a workload")
				}
			}
		}
		if !r.IdentityKnown {
			r.Reasons = append(r.Reasons, "process identity incomplete; executable, arguments, or start time unavailable")
		}
		// A partial known sum can prove activity; missing samples cannot prove idle.
		if r.CPU >= derive.BackgroundTreeCPUPercent {
			protect("last observed CPU indicates activity across the process tree")
		}
		if !r.CPUKnown {
			r.Reasons = append(r.Reasons, "CPU observation unavailable for one or more tree members")
		}
		if !r.PeersKnown {
			r.Reasons = append(r.Reasons, "socket observation unavailable for one or more tree members")
		}
		if reason := derive.OrphanOwnershipReason(idx, in.Sessions, root.PID); reason != "" {
			protect(reason)
		}
		if reviewRememberedOwner(in, root, idx) {
			protect("remembered spawning session is still live")
		}
		if reason := derive.NativeAgentOwnershipReason(idx, tree, nativeAgents, in.Self, in.SourceOS); reason != "" {
			protect(reason)
		}
		r.Reasons = reviewUnique(r.Reasons)
		rows = append(rows, r)
	}
	return rows
}

func reviewDetached(idx derive.ProcIndex, p model.Proc, sourceOS string) bool {
	parent, ok := idx.Get(p.PPID)
	if sourceOS == "windows" {
		return !ok || !parent.StartTime.IsZero() && !p.StartTime.IsZero() && parent.StartTime.After(p.StartTime)
	}
	return p.PPID == 1 || p.PPID > 1 && !ok
}

func reviewBasename(s string) string {
	s = path.Base(strings.ReplaceAll(s, "\\", "/"))
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	if s == "" || s == "." || s == "/" {
		return "unknown"
	}
	return s
}

func reviewCategory(p model.Proc, classifier *derive.Classifier) (string, string) {
	name := reviewBasename(p.Exe)
	if name == "unknown" && len(p.Argv) > 0 {
		name = reviewBasename(p.Argv[0])
	}
	base := strings.TrimSuffix(strings.ToLower(name), ".exe")
	exe := strings.ToLower(strings.ReplaceAll(p.Exe, "\\", "/"))
	if p.Role == "service" || p.Role == "internal" || p.Role == "app" || p.Role == "chat" || strings.Contains(exe, ".app/") || strings.HasPrefix(exe, "/system/") || strings.HasPrefix(exe, "/usr/libexec/") || strings.HasPrefix(exe, "/usr/sbin/") || strings.HasPrefix(exe, "/sbin/") || strings.Contains(exe, "/windows/system32/") || strings.Contains(exe, "/program files/") || strings.Contains(exe, "/program files (x86)/") {
		return "managed", name
	}
	switch base {
	case "process-compose", "adb", "systemd", "launchd", "init", "kernel_task", "svchost", "services", "lsass", "csrss", "wininit", "winlogon", "windowserver", "driga", "ssh-agent", "gpg-agent", "dbus-daemon", "dbus-broker":
		return "managed", name
	case "postgres", "postmaster", "mysqld", "mariadbd", "mongod", "redis-server":
		for i, arg := range p.Argv {
			dir := ""
			if (arg == "-D" || arg == "--datadir" || arg == "--dbpath" || arg == "--dir") && i+1 < len(p.Argv) {
				dir = p.Argv[i+1]
			}
			for _, prefix := range []string{"--datadir=", "--dbpath=", "--dir="} {
				if strings.HasPrefix(arg, prefix) {
					dir = strings.TrimPrefix(arg, prefix)
				}
			}
			if reviewTemporary(dir) {
				return "test_database", name
			}
		}
		return "database", name
	}
	if sig, ok := classifier.Match(p); ok {
		if sig.ProtectTree || sig.Role == signatures.RoleInternal || sig.Role == signatures.RoleService || sig.Role == signatures.RoleApp {
			return "managed", name
		}
		if sig.Role == signatures.RoleHelper || sig.Role == signatures.RoleHost || sig.Role == signatures.RoleHeadless {
			return "agent_helper", name
		}
	}
	if p.Role == "helper" || p.Role == "host" || p.Role == "headless" || strings.HasPrefix(base, "mcp-") || strings.Contains(base, "mcp-server") {
		return "agent_helper", name
	}
	switch base {
	case "astro", "vite", "next", "webpack", "uvicorn":
		return "dev_server", name
	}
	serverMode := false
	for _, arg := range p.Argv[minimumArgOffset(p.Argv):] {
		if arg == "-c" || arg == "-lc" || arg == "-e" || arg == "--eval" || arg == "-Command" {
			break
		}
		if arg == "serve" || arg == "server" || arg == "--serve" || arg == "--server" {
			serverMode = true
		}
	}
	for _, arg := range p.Argv[minimumArgOffset(p.Argv):] {
		// Inline interpreter payloads are not paths or safe display names.
		if arg == "-c" || arg == "-lc" || arg == "-e" || arg == "--eval" || arg == "-Command" {
			break
		}
		if strings.HasPrefix(arg, "-") || strings.ContainsAny(arg, "\t\r\n ") {
			continue
		}
		b := strings.ToLower(reviewBasename(arg))
		if strings.HasSuffix(b, ".py") || strings.HasSuffix(b, ".sh") || strings.HasSuffix(b, ".rb") || strings.HasSuffix(b, ".ps1") {
			return "script", reviewBasename(arg)
		}
		stem := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(b, ".mjs"), ".js"), ".ts")
		jsScript := strings.HasSuffix(b, ".js") || strings.HasSuffix(b, ".mjs") || strings.HasSuffix(b, ".ts")
		if jsScript && (strings.Contains(stem, "server") || serverMode) || stem == "astro" || stem == "vite" || stem == "next" || stem == "webpack" || b == "http.server" || b == "uvicorn" {
			return "dev_server", reviewBasename(arg)
		}
		if jsScript {
			return "script", reviewBasename(arg)
		}
	}
	return "unknown", name
}

func minimumArgOffset(args []string) int {
	if len(args) > 0 {
		return 1
	}
	return 0
}

func reviewTemporary(s string) bool {
	s = strings.ToLower(strings.ReplaceAll(s, "\\", "/"))
	return strings.HasPrefix(s, "/tmp/") || strings.HasPrefix(s, "/private/tmp/") || strings.HasPrefix(s, "/var/tmp/") || strings.HasPrefix(s, "/var/folders/") || strings.HasPrefix(s, "/private/var/folders/") || len(s) > 3 && s[1] == ':' && (strings.HasPrefix(s[2:], "/temp/") || strings.Contains(s, "/appdata/local/temp/"))
}

func reviewRememberedOwner(in ProcessReviewInput, root model.Proc, idx derive.ProcIndex) bool {
	for _, o := range in.Orphans {
		if o.RootPID != root.PID || root.StartTime.IsZero() || !derive.SameStart(o.StartTime, root.StartTime) {
			continue
		}
		for _, evidence := range o.Evidence {
			_, suffix, ok := strings.Cut(evidence, "spawned by session ")
			if !ok {
				continue
			}
			fields := strings.Fields(suffix)
			if len(fields) == 0 {
				continue
			}
			id, err := strconv.ParseInt(fields[0], 10, 64)
			if err != nil {
				continue
			}
			for _, s := range in.Sessions {
				if s.ID != id || s.EndedAt != nil || s.Label == "dead" || s.Label == "ended" {
					continue
				}
				p, ok := idx.Get(s.PID)
				if s.Hosted && s.PID == 0 || ok && !s.ProcStart.IsZero() && derive.SameStart(p.StartTime, s.ProcStart) {
					return true
				}
			}
		}
	}
	return false
}

func reviewUnique(reasons []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, r := range reasons {
		if !seen[r] {
			result = append(result, r)
			seen[r] = true
		}
	}
	return result
}
