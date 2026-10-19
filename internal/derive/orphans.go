package derive

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
)

// OrphanOption supplies remembered observations from earlier process passes.
type OrphanOption func(*orphanOptions)
type orphanOptions struct {
	spawnMemory   map[string]int64
	externalPeers map[int32]bool
	subreaper     int32
}

// WithSubreaper recognizes helpers reparented to the user's systemd instance.
func WithSubreaper(pid int32) OrphanOption {
	return func(o *orphanOptions) {
		if pid > 1 {
			o.subreaper = pid
		}
	}
}

// WithExternalPeers supplies a fresh SocketProber result for each candidate tree.
// Collectors must probe every tree pid each process pass, pass only successful
// results here, and use the immediately preceding pass as prev. Missing results
// leave candidates unverified. Verified also needs persistence in stored views.
func WithExternalPeers(peers map[int32]bool) OrphanOption {
	return func(o *orphanOptions) { o.externalPeers = peers }
}

// WithSpawnMemory supplies first-sighting ownership, keyed by pid:startunix.
func WithSpawnMemory(memory map[string]int64) OrphanOption {
	return func(o *orphanOptions) { o.spawnMemory = memory }
}

// FindOrphans finds idle helper trees whose parent or recorded owner is gone.
// prev preserves first-seen, spawning evidence and the previous idle observation.
// CPUPercent represents the sampled window; a missing socket probe never verifies.
func FindOrphans(idx ProcIndex, sessions []model.Session, sigs *signatures.Set, now time.Time, minAge time.Duration, prev map[string]model.Orphan, opts ...OrphanOption) []model.Orphan {
	var options orphanOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	livePIDs := familyLivePIDs(sessions, idx)
	liveIDs := make(map[int64]bool)
	for _, id := range livePIDs {
		liveIDs[id] = true
	}
	for _, s := range sessions {
		if familySessionLive(s) && s.PID == 0 && s.Hosted {
			liveIDs[s.ID] = true
		}
	}
	apps := make(map[string]bool)
	if sigs != nil {
		for _, pid := range idx.pids {
			p := idx.procs[pid]
			if sig, ok := sigs.Match(p.Cmdline, p.Exe); ok && sig.Role == signatures.RoleApp {
				if bundle := orphanBundle(p); bundle != "" {
					apps[bundle] = true
				}
			}
		}
	}
	candidates := make(map[int32]model.Orphan)
	for _, pid := range idx.pids {
		r := idx.procs[pid]
		internal := r.Role == string(signatures.RoleInternal)
		for _, ancestor := range idx.Ancestors(pid) {
			if idx.procs[ancestor].Role == string(signatures.RoleInternal) {
				internal = true
				break
			}
		}
		if internal {
			continue
		}
		if _, ok := livePIDs[pid]; ok {
			continue
		}
		id := fmt.Sprintf("%d:%d", pid, r.StartTime.Unix())
		old, existed := prev[id]
		owner, remembered := options.spawnMemory[id]
		spawned := remembered
		deadEvidence := false
		for _, evidence := range old.Evidence {
			if strings.Contains(evidence, "spawned by session") {
				spawned = true
				if !remembered {
					text := strings.TrimSpace(strings.SplitN(evidence, "spawned by session", 2)[1])
					if fields := strings.Fields(text); len(fields) > 0 {
						owner, _ = strconv.ParseInt(fields[0], 10, 64)
					}
				}
			}
			if strings.Contains(evidence, "spawning session") && strings.Contains(evidence, "dead") {
				deadEvidence = true
			}
		}
		ownerDead := deadEvidence
		if spawned && owner != 0 {
			ownerDead = !liveIDs[owner]
		}
		if r.PPID != 1 && (options.subreaper <= 1 || r.PPID != options.subreaper) && !ownerDead {
			continue
		}
		idle := true
		probed := true
		var matched string
		// visit applies the idle checks to one tree member; false stops the walk.
		visit := func(p model.Proc) bool {
			if p.Role == string(signatures.RoleInternal) {
				idle = false
				return false
			}
			if p.CPUPercent != 0 || options.externalPeers[p.PID] {
				idle = false
				return false
			}
			if _, ok := options.externalPeers[p.PID]; !ok {
				probed = false
			}
			if _, ok := livePIDs[p.PID]; ok {
				idle = false
				return false
			}
			if bundle := orphanBundle(p); bundle != "" && apps[bundle] {
				idle = false
				return false
			}
			if sigs != nil && matched == "" {
				if sig, ok := sigs.Match(p.Cmdline, p.Exe); ok && (sig.Role == signatures.RoleHelper || sig.Role == signatures.RoleHost || sig.Role == signatures.RoleHeadless) {
					matched = sig.Name
				}
			}
			return true
		}
		// A busy root settles the tree, so its descendants are walked only after it passes.
		tree := []model.Proc{r}
		if visit(r) {
			desc := idx.Descendants(pid)
			for _, p := range desc {
				if !visit(p) {
					break
				}
			}
			tree = append(tree, desc...)
		}
		if !idle || (matched == "" && !spawned) {
			continue
		}
		o := model.Orphan{ID: id, RootPID: pid, StartTime: r.StartTime, Signature: matched, Cwd: r.Cwd, Young: now.Sub(r.StartTime) < minAge, FirstSeen: now}
		if existed && !old.FirstSeen.IsZero() {
			o.FirstSeen = old.FirstSeen
		}
		if r.PPID == 1 {
			o.Evidence = append(o.Evidence, "ppid=1")
		}
		if options.subreaper > 1 && r.PPID == options.subreaper {
			o.Evidence = append(o.Evidence, "reparented to systemd --user")
		}
		if matched != "" {
			o.Evidence = append(o.Evidence, matched)
		}
		if spawned {
			evidence := "spawned by session"
			if owner != 0 {
				evidence += fmt.Sprintf(" %d", owner)
			}
			o.Evidence = append(o.Evidence, evidence)
		}
		if ownerDead {
			o.Evidence = append(o.Evidence, "spawning session dead")
		}
		for _, s := range sessions {
			if liveIDs[s.ID] && r.Cwd != "" && r.Cwd == s.Cwd {
				o.Evidence = append(o.Evidence, "live session in same dir")
				break
			}
		}
		for _, p := range tree {
			o.RSSTotal += p.RSS
			o.PIDs = append(o.PIDs, p.PID)
		}
		o.ProcCount = len(o.PIDs)
		if probed {
			const idleEvidence = "no CPU or external peers"
			o.Evidence = append(o.Evidence, idleEvidence)
			o.Verified = existed && old.StartTime.Equal(o.StartTime) &&
				slices.Equal(old.PIDs, o.PIDs) && slices.Contains(old.Evidence, idleEvidence)
		}
		candidates[pid] = o
	}
	var result []model.Orphan
	for _, pid := range idx.pids {
		o, ok := candidates[pid]
		if !ok {
			continue
		}
		suppressed := false
		for _, ancestor := range idx.Ancestors(pid) {
			if _, ok := candidates[ancestor]; ok {
				// For a malformed cycle, keep its smallest candidate PID.
				cycle := false
				for _, back := range idx.Ancestors(ancestor) {
					if back == pid {
						cycle = true
						break
					}
				}
				if !cycle || ancestor < pid {
					suppressed = true
					break
				}
			}
		}
		if !suppressed {
			result = append(result, o)
		}
	}
	return result
}

func orphanBundle(p model.Proc) string {
	if bundle, ok := signatures.BundlePath(p.Cmdline); ok {
		return bundle
	}
	bundle, _ := signatures.BundlePath(p.Exe)
	return bundle
}
