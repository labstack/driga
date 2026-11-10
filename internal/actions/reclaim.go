package actions

import (
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/reclaim"
)

func recordKillOutcome(started time.Time, outcome KillOutcome) error {
	entry := reclaim.Entry{At: time.Now().UTC(), StartedAt: started, Kind: "process", ID: outcome.ID, MachineID: outcome.MachineID, RootPID: outcome.RootPID, Status: outcome.Status, LastObservedCPU: outcome.LastObservedCPU}
	if outcome.Error != nil {
		entry.ErrorCode = outcome.Error.Code
	}
	if r := outcome.Result; r != nil {
		entry.Signaled, entry.Stopped, entry.Survivors, entry.Unknown = r.Signaled, r.Stopped, r.Survivors, r.Unknown
		entry.Before, entry.StoppedUsage = r.Before, r.StoppedUsage
	}
	return reclaim.Append(config.DataDir(), entry)
}
