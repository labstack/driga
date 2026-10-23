package model

import (
	"encoding/json"
	"time"

	"github.com/labstack/driga/internal/jsonx"
)

func (v Machine) MarshalJSON() ([]byte, error) { type plain Machine; return jsonx.Marshal(plain(v)) }
func (v Proc) MarshalJSON() ([]byte, error) {
	type plain Proc
	return jsonx.Object(plain(v), struct {
		CPU int64 `json:"cpu_ms"`
	}{int64(v.CPUSeconds * 1000)})
}
func (v MachineSample) MarshalJSON() ([]byte, error) {
	type plain MachineSample
	return jsonx.Marshal(plain(v))
}
func (v Repo) MarshalJSON() ([]byte, error)     { type plain Repo; return jsonx.Marshal(plain(v)) }
func (v Worktree) MarshalJSON() ([]byte, error) { type plain Worktree; return jsonx.Marshal(plain(v)) }
func (v SessionObs) MarshalJSON() ([]byte, error) {
	type plain SessionObs
	return jsonx.Marshal(plain(v))
}
func (v CompletionEvent) MarshalJSON() ([]byte, error) {
	type plain CompletionEvent
	return jsonx.Marshal(plain(v))
}
func (v FileTouch) MarshalJSON() ([]byte, error) {
	type plain FileTouch
	return jsonx.Marshal(plain(v))
}
func (v HookEvent) MarshalJSON() ([]byte, error) {
	type plain HookEvent
	return jsonx.Marshal(plain(v))
}
func (v Summary) MarshalJSON() ([]byte, error) { type plain Summary; return jsonx.Marshal(plain(v)) }
func (v Session) MarshalJSON() ([]byte, error) {
	v.Label, v.Heuristic = jsonx.Label(v.Label, v.Heuristic)
	return jsonx.Object(v.SessionObs, struct {
		StartedFrom string      `json:"started_from,omitempty"`
		Mixer       *StateMixer `json:"mixer,omitzero"`
		MachineID   string      `json:"machine_id"`
		ID          int64       `json:"id"`
		ParentID    *int64      `json:"parent_id,omitzero"`
		RepoID      *int64      `json:"repo_id,omitzero"`
		WorktreeID  *int64      `json:"worktree_id,omitzero"`
		Label       string      `json:"label"`
		Heuristic   bool        `json:"heuristic"`
		Evidence    string      `json:"evidence"`
		EndedAt     *time.Time  `json:"ended_at,omitzero"`
	}{v.StartedFrom, v.Mixer, v.MachineID, v.ID, v.ParentID, v.RepoID, v.WorktreeID, v.Label, v.Heuristic, v.Evidence, v.EndedAt})
}
func (v InboxItem) MarshalJSON() ([]byte, error) {
	type plain InboxItem
	return jsonx.Marshal(plain(v))
}
func (v Orphan) MarshalJSON() ([]byte, error)   { type plain Orphan; return jsonx.Marshal(plain(v)) }
func (v Conflict) MarshalJSON() ([]byte, error) { type plain Conflict; return jsonx.Marshal(plain(v)) }
func (v Offset) MarshalJSON() ([]byte, error)   { type plain Offset; return jsonx.Marshal(plain(v)) }
func (v SourceStatus) MarshalJSON() ([]byte, error) {
	type plain SourceStatus
	return jsonx.Marshal(plain(v))
}
func (v Gap) MarshalJSON() ([]byte, error)   { type plain Gap; return jsonx.Marshal(plain(v)) }
func (v Event) MarshalJSON() ([]byte, error) { type plain Event; return jsonx.Marshal(plain(v)) }

// CPU samples use fractional seconds internally and integer milliseconds on wire.
func (v *Proc) UnmarshalJSON(b []byte) error {
	type plain Proc
	var row plain
	if err := json.Unmarshal(b, &row); err != nil {
		return err
	}
	row.CPUSeconds /= 1000
	*v = Proc(row)
	return nil
}
