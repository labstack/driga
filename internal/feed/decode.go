package feed

import (
	"encoding/json"
	"fmt"

	"github.com/labstack/driga/internal/model"
)

// ProcRow attributes a process through a natural session reference.
type ProcRow struct {
	model.Proc
	Session *SessionKey `json:"session,omitempty"`
}

// DecodedProcs is the wire representation of a replacement process snapshot.
type DecodedProcs struct {
	Processes []ProcRow `json:"processes"`
}

// Decode parses a known feed kind without interpreting peer database IDs.
func Decode(kind Kind, payload []byte) (any, error) {
	var row any
	switch kind {
	case Machine:
		row = new(model.Machine)
	case Session:
		row = new(SessionRow)
	case SessionFiles:
		row = new(FilesRow)
	case Event:
		row = new(model.Event)
	case Handoff:
		row = new(HandoffRow)
	case Inbox:
		row = new(InboxRow)
	case Repo:
		row = new(model.Repo)
	case Worktree:
		row = new(WorktreeRow)
	case Orphan:
		row = new(model.Orphan)
	case Procs:
		row = new(DecodedProcs)
	case MachineSample:
		row = new(model.MachineSample)
	case Source:
		row = new(model.SourceStatus)
	case Gap:
		row = new(model.Gap)
	case Delete:
		row = new(DeleteRow)
	default:
		return nil, fmt.Errorf("unsupported feed kind %q", kind)
	}
	if len(payload) == 0 || string(payload) == "null" {
		return nil, fmt.Errorf("missing %s payload", kind)
	}
	if err := json.Unmarshal(payload, row); err != nil {
		return nil, fmt.Errorf("decode %s: %w", kind, err)
	}
	return row, nil
}
