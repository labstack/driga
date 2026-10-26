package collector

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/labstack/driga/internal/adapter/copilot"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
)

// Reconcile captured identities even on spool-only wakes. A PID without its
// recorded start is never linked, and a provisional process row is retired
// once the actual hook session becomes known.
func reconcileCopilot(ss []model.Session, plat platform.Platform, now time.Time) {
	for i := range ss {
		s := &ss[i]
		if s.Adapter != "copilot" || strings.HasPrefix(s.ExternalID, "pid:") {
			continue
		}
		if s.PID != 0 && (s.ProcStart.IsZero() || !plat.Alive(s.PID, s.ProcStart)) {
			s.PID = 0
		}
		if s.RegistryStatus == "ended" {
			at := s.LastActivityAt
			s.EndedAt = &at
		} else if s.PID != 0 || s.Hosted && copilot.Recent(s.SessionObs, now) {
			s.EndedAt = nil
		} else if s.EndedAt == nil {
			at := now
			s.EndedAt = &at
		}
		for j := range ss {
			p := &ss[j]
			if p.Adapter == "copilot" && strings.HasPrefix(p.ExternalID, "pid:") && p.PID != 0 && p.PID == s.PID && platform.SameStart(p.ProcStart, s.ProcStart) {
				p.PID = 0
				at := now
				p.EndedAt = &at
			}
		}
	}
}

func copilotCapturePolicy(ss []model.Session, policy string) {
	for i := range ss {
		s := &ss[i]
		if s.Adapter != "copilot" {
			continue
		}
		if policy != "snippets" {
			s.LastPrompt = ""
			s.Title = ""
			if s.Cwd != "" {
				s.Title = filepath.Base(s.Cwd)
			}
		}
	}
}

func labelCopilot(s *model.Session, procs []model.Proc, unseen bool, now time.Time, stuckAfter time.Duration) {
	in := derive.StateInput{Obs: s.SessionObs, Unseen: unseen, LastUserPrompt: s.LastPromptAt}
	for _, p := range procs {
		if p.PID == s.PID && !s.ProcStart.IsZero() && platform.SameStart(p.StartTime, s.ProcStart) {
			in.Proc = &p
			break
		}
	}
	mixed := derive.LabelWithEvidence(in, now, derive.Thresholds{StuckAfter: stuckAfter})
	s.Mixer = &mixed
	s.Label, s.Heuristic, s.Evidence = mixed.Verdict, mixed.Heuristic, mixed.Evidence
}
