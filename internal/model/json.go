package model

import (
	"encoding/json"
	"github.com/labstack/driga/internal/jsonwire"
)

func (v Machine) MarshalJSON() ([]byte, error)         { return jsonwire.Marshal(v) }
func (v Proc) MarshalJSON() ([]byte, error)            { return jsonwire.Marshal(v) }
func (v MachineSample) MarshalJSON() ([]byte, error)   { return jsonwire.Marshal(v) }
func (v Repo) MarshalJSON() ([]byte, error)            { return jsonwire.Marshal(v) }
func (v Worktree) MarshalJSON() ([]byte, error)        { return jsonwire.Marshal(v) }
func (v SessionObs) MarshalJSON() ([]byte, error)      { return jsonwire.Marshal(v) }
func (v CompletionEvent) MarshalJSON() ([]byte, error) { return jsonwire.Marshal(v) }
func (v FileTouch) MarshalJSON() ([]byte, error)       { return jsonwire.Marshal(v) }
func (v HookEvent) MarshalJSON() ([]byte, error)       { return jsonwire.Marshal(v) }
func (v Summary) MarshalJSON() ([]byte, error)         { return jsonwire.Marshal(v) }
func (v Session) MarshalJSON() ([]byte, error)         { return jsonwire.Marshal(v) }
func (v InboxItem) MarshalJSON() ([]byte, error)       { return jsonwire.Marshal(v) }
func (v Orphan) MarshalJSON() ([]byte, error)          { return jsonwire.Marshal(v) }
func (v Conflict) MarshalJSON() ([]byte, error)        { return jsonwire.Marshal(v) }
func (v Offset) MarshalJSON() ([]byte, error)          { return jsonwire.Marshal(v) }
func (v SourceStatus) MarshalJSON() ([]byte, error)    { return jsonwire.Marshal(v) }
func (v Gap) MarshalJSON() ([]byte, error)             { return jsonwire.Marshal(v) }
func (v Event) MarshalJSON() ([]byte, error)           { return jsonwire.Marshal(v) }

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
