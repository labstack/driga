package store

import (
	"github.com/labstack/driga/internal/model"
	"time"
)

// MachineReader exposes reads for one machine without exposing writes.
type MachineReader struct{ machineReads }

type machineReads interface {
	MachineID() string
	Machines() ([]model.Machine, error)
	ReadMachine(string) *MachineReader
	Sessions(SessionFilter) ([]model.Session, error)
	Session(int64) (model.Session, error)
	Procs() ([]model.Proc, error)
	Repos() ([]model.Repo, error)
	Worktrees(int64) ([]model.Worktree, error)
	Handoffs(int64, int) ([]model.CompletionEvent, error)
	SessionHandoffs(int64, time.Time, time.Time) ([]model.CompletionEvent, error)
	Summaries(int64) ([]model.Summary, error)
	SessionFiles(time.Time) (map[int64][]model.FileTouch, error)
	Events(string, string, time.Time) ([]model.Event, error)
	RecentEvents(time.Time, int) ([]model.Event, error)
	Inbox(time.Time, bool) ([]model.InboxItem, error)
	Orphans() ([]model.Orphan, error)
	LatestSample() (model.MachineSample, error)
	Samples(time.Time) ([]model.MachineSample, error)
	EventCounts(time.Time, time.Time) ([]EventCount, error)
	ObservationMinutes(int64, time.Time) ([]ObservationMinute, error)
	ScanMCPHistory(time.Time, time.Time, func(model.MCPObservation) error) error
	Sources() ([]model.SourceStatus, error)
	Gaps(time.Time) ([]model.Gap, error)
}

// ReadMachine reuses this connection or transaction with a machine-local scope.
func (q *querier) ReadMachine(id string) *MachineReader {
	copy := *q
	copy.machineID = id
	return &MachineReader{machineReads: &copy}
}
