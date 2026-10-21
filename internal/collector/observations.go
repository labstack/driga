package collector

import (
	"math"
	"os"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

type logBaseline struct {
	path string
	info os.FileInfo
}

// observeSessions stats files only. Its baselines advance with the pass commit,
// so a failed transaction can be retried without losing measured growth.
func observeSessions(tx *store.Tx, work *State, ss []model.Session, procs []model.Proc, previous map[int32]model.Proc, fresh bool, now time.Time) error {
	next := make(map[string]logBaseline, len(ss))
	byPID := make(map[int32]model.Proc, len(procs))
	owners := make(map[int32]int, len(ss))
	for _, s := range ss {
		if s.PID != 0 {
			owners[s.PID]++
		}
	}
	for _, p := range procs {
		byPID[p.PID] = p
	}
	for _, s := range ss {
		key := sessionKey(s.Adapter, s.ExternalID)
		var growth, cpu int64
		if s.SourcePath != "" {
			if info, err := os.Stat(s.SourcePath); err == nil && info.Mode().IsRegular() {
				prev, ok := work.logs[key]
				if ok && prev.path == s.SourcePath && os.SameFile(prev.info, info) {
					growth = max(0, info.Size()-prev.info.Size())
				}
				next[key] = logBaseline{s.SourcePath, info}
			}
		}
		// Attribute only a session's own process. Shared hosts and helper trees
		// cannot honestly supply per-session CPU measurements.
		cur, ok := byPID[s.PID]
		prev, had := previous[s.PID]
		if fresh && ok && had && owners[s.PID] == 1 && (s.ProcStart.IsZero() || derive.SameStart(cur.StartTime, s.ProcStart)) && prev.StartTime.Equal(cur.StartTime) {
			// Quantize cumulative counters before subtraction. Rounding each
			// small delta independently would lose or manufacture CPU over time.
			ms := math.Round(cur.CPUSeconds*1000) - math.Round(prev.CPUSeconds*1000)
			if ms > 0 && !math.IsNaN(ms) && !math.IsInf(ms, 0) && ms < float64(math.MaxInt64) {
				cpu = int64(ms)
			}
		}
		if err := tx.Observe(s.ID, now, growth, cpu); err != nil {
			return err
		}
	}
	work.logs = next
	return nil
}
