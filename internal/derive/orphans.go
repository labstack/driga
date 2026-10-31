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

// BackgroundCPUPercent is the per-process share of one core below which an
// orphan tree member counts as idle, and BackgroundTreeCPUPercent bounds the
// whole tree. Dead helpers still tick timers and GC far below these; real work
// runs far above them. External peers stay the liveness proof for work that
// waits on the network.
const (
	BackgroundCPUPercent     = 5.0
	BackgroundTreeCPUPercent = 25.0
)

// IdleWithoutClients reports whether p's signature sets IdleWithoutClients.
func IdleWithoutClients(c *Classifier, p model.Proc) bool {
	if c == nil {
		return false
	}
	sig, ok := c.Match(p)
	return ok && sig.IdleWithoutClients
}

// NeedsResolvedPeers reports whether InUse may waive a socket in tree, so its
// probe must tell a listener's clients and system services apart
// (platform.PeerResolver).
func NeedsResolvedPeers(c *Classifier, tree []model.Proc) bool {
	for _, p := range tree {
		if IdleWithoutClients(c, p) {
			return true
		}
	}
	return false
}

// InUse reports whether member p's socket state shows its tree still serves
// someone. A listener counts unless p's own signature sets IdleWithoutClients;
// a system-service socket counts unless the tree root's does.
func InUse(c *Classifier, root, p model.Proc, state model.PeerState) bool {
	switch state {
	case model.PeerConnected:
		return true
	case model.PeerListening:
		return !IdleWithoutClients(c, p)
	case model.PeerSystem:
		return !IdleWithoutClients(c, root)
	}
	return false
}

// OrphanOption supplies remembered observations from earlier process passes.
type OrphanOption func(*orphanOptions)
type orphanOptions struct {
	spawnMemory   map[string]int64
	externalPeers map[int32]model.PeerState
	subreaper     int32
	classifier    *Classifier
}

// WithClassifier shares matches with other derivations of this process pass.
func WithClassifier(c *Classifier) OrphanOption {
	return func(o *orphanOptions) { o.classifier = c }
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
func WithExternalPeers(peers map[int32]model.PeerState) OrphanOption {
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
	classifier := options.classifier
	if classifier == nil && sigs != nil {
		classifier = NewClassifier(sigs, len(idx.pids))
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
	protected := make(map[int32]bool)
	for _, pid := range idx.pids {
		p := idx.procs[pid]
		protected[pid] = p.Role == string(signatures.RoleInternal) || p.Role == string(signatures.RoleService)
		if sigs != nil {
			if sig, ok := classifier.Match(p); ok {
				protected[pid] = protected[pid] || sig.Role == signatures.RoleInternal || sig.Role == signatures.RoleService || sig.ProtectTree
				if sig.Role != signatures.RoleApp {
					continue
				}
				if bundle := orphanBundle(p); bundle != "" {
					apps[bundle] = true
				}
			}
		}
	}
	candidates := make(map[int32]model.Orphan)
	for _, pid := range idx.pids {
		r := idx.procs[pid]
		managed := protected[pid]
		for _, ancestor := range idx.Ancestors(pid) {
			if protected[ancestor] {
				managed = true
				break
			}
		}
		if managed {
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
		parentEvidence := ""
		if idx.windows {
			if r.StartTime.IsZero() {
				continue
			}
			parent, present := idx.Get(r.PPID)
			switch {
			case !present:
				parentEvidence = "parent process gone"
			case !parent.StartTime.IsZero() && parent.StartTime.After(r.StartTime):
				parentEvidence = "parent PID reused (parent started after child)"
			default:
				continue
			}
		} else if r.PPID != 1 && (options.subreaper <= 1 || r.PPID != options.subreaper) && !ownerDead {
			continue
		}
		idle := true
		probed := true
		var matched string
		var treeCPU float64
		// visit applies the idle checks to one tree member; false stops the walk.
		visit := func(p model.Proc) bool {
			if protected[p.PID] {
				idle = false
				return false
			}
			treeCPU += p.CPUPercent
			if p.CPUPercent >= BackgroundCPUPercent || treeCPU >= BackgroundTreeCPUPercent || InUse(classifier, r, p, options.externalPeers[p.PID]) {
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
			// A process from a running app's bundle belongs to that app, unless an
			// agent helper started it to borrow the bundle's runtime.
			if bundle := orphanBundle(p); bundle != "" && apps[bundle] && (p.PID == pid || !agentHelper(classifier, idx, p.PPID)) {
				idle = false
				return false
			}
			if sigs != nil && matched == "" {
				if sig, ok := classifier.Match(p); ok && (sig.Role == signatures.RoleHelper || sig.Role == signatures.RoleHost || sig.Role == signatures.RoleHeadless) {
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
		if parentEvidence != "" {
			o.Evidence = append(o.Evidence, parentEvidence)
		}
		if !idx.windows && r.PPID == 1 {
			o.Evidence = append(o.Evidence, "ppid=1")
		}
		if !idx.windows && options.subreaper > 1 && r.PPID == options.subreaper {
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
			if idx.windows {
				if o.ProcessStarts == nil {
					o.ProcessStarts = make(map[int32]time.Time)
				}
				o.ProcessStarts[p.PID] = p.StartTime
				if p.StartTime.IsZero() {
					probed = false
				}
			}
		}
		o.ProcCount = len(o.PIDs)
		if probed {
			const idleEvidence = "background CPU only, no external peers"
			o.Evidence = append(o.Evidence, idleEvidence)
			o.Verified = existed && old.StartTime.Equal(o.StartTime) &&
				slices.Equal(old.PIDs, o.PIDs) && slices.Contains(old.Evidence, idleEvidence)
			if idx.windows {
				for pid, start := range o.ProcessStarts {
					if !start.Equal(old.ProcessStarts[pid]) {
						o.Verified = false
					}
				}
			}
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

// agentHelper reports whether pid is a known agent helper, host or headless run.
func agentHelper(c *Classifier, idx ProcIndex, pid int32) bool {
	parent, ok := idx.Get(pid)
	if !ok || c == nil {
		return false
	}
	sig, ok := c.Match(parent)
	return ok && (sig.Role == signatures.RoleHelper || sig.Role == signatures.RoleHost || sig.Role == signatures.RoleHeadless)
}

func orphanBundle(p model.Proc) string {
	if bundle, ok := signatures.BundlePath(p.Cmdline); ok {
		return bundle
	}
	bundle, _ := signatures.BundlePath(p.Exe)
	return bundle
}
