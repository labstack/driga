package view

import "github.com/labstack/driga/internal/jsonwire"

func (v TailEvent) MarshalJSON() ([]byte, error)       { return jsonwire.Marshal(v) }
func (v ActivityBucket) MarshalJSON() ([]byte, error)  { return jsonwire.Marshal(v) }
func (v ActivityMachine) MarshalJSON() ([]byte, error) { return jsonwire.Marshal(v) }
func (v ActivityView) MarshalJSON() ([]byte, error)    { return jsonwire.Marshal(v) }
func (v Fresh) MarshalJSON() ([]byte, error)           { return jsonwire.Marshal(v) }
func (v Envelope) MarshalJSON() ([]byte, error)        { return jsonwire.Marshal(v) }
func (v AgentRow) MarshalJSON() ([]byte, error)        { return jsonwire.Marshal(v) }
func (v AgentsView) MarshalJSON() ([]byte, error)      { return jsonwire.Marshal(v) }
func (v AgentView) MarshalJSON() ([]byte, error)       { return jsonwire.Marshal(v) }
func (v FileRow) MarshalJSON() ([]byte, error)         { return jsonwire.Marshal(v) }
func (v ProcRow) MarshalJSON() ([]byte, error)         { return jsonwire.Marshal(v) }
func (v EventRow) MarshalJSON() ([]byte, error)        { return jsonwire.Marshal(v) }
func (v WorktreeRow) MarshalJSON() ([]byte, error)     { return jsonwire.Marshal(v) }
func (v HandoffRow) MarshalJSON() ([]byte, error)      { return jsonwire.Marshal(v) }
func (v ProjectStatus) MarshalJSON() ([]byte, error)   { return jsonwire.Marshal(v) }
func (v WherePart) MarshalJSON() ([]byte, error)       { return jsonwire.Marshal(v) }
func (v ProjectRow) MarshalJSON() ([]byte, error)      { return jsonwire.Marshal(v) }
func (v ProjectsView) MarshalJSON() ([]byte, error)    { return jsonwire.Marshal(v) }
func (v WhereView) MarshalJSON() ([]byte, error)       { return jsonwire.Marshal(v) }
func (v InboxRow) MarshalJSON() ([]byte, error)        { return jsonwire.Marshal(v) }
func (v InboxView) MarshalJSON() ([]byte, error)       { return jsonwire.Marshal(v) }
func (v ConflictRow) MarshalJSON() ([]byte, error)     { return jsonwire.Marshal(v) }
func (v ConflictsView) MarshalJSON() ([]byte, error)   { return jsonwire.Marshal(v) }
func (v OrphanRow) MarshalJSON() ([]byte, error)       { return jsonwire.Marshal(v) }
func (v OrphansView) MarshalJSON() ([]byte, error)     { return jsonwire.Marshal(v) }
func (v MemoryRow) MarshalJSON() ([]byte, error)       { return jsonwire.Marshal(v) }
func (v MachineView) MarshalJSON() ([]byte, error)     { return jsonwire.Marshal(v) }
func (v SummaryView) MarshalJSON() ([]byte, error)     { return jsonwire.Marshal(v) }
func (v MachineFresh) MarshalJSON() ([]byte, error)    { return jsonwire.Marshal(v) }
func (v MachineRow) MarshalJSON() ([]byte, error)      { return jsonwire.Marshal(v) }
func (v MachinesView) MarshalJSON() ([]byte, error)    { return jsonwire.Marshal(v) }
