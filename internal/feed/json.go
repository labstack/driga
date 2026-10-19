package feed

import "github.com/labstack/driga/internal/jsonwire"

func (v FeedRow) MarshalJSON() ([]byte, error)       { return jsonwire.Marshal(v) }
func (v SessionKey) MarshalJSON() ([]byte, error)    { return jsonwire.Marshal(v) }
func (v SessionRow) MarshalJSON() ([]byte, error)    { return jsonwire.Marshal(v) }
func (v FilesRow) MarshalJSON() ([]byte, error)      { return jsonwire.Marshal(v) }
func (v HandoffRow) MarshalJSON() ([]byte, error)    { return jsonwire.Marshal(v) }
func (v InboxRow) MarshalJSON() ([]byte, error)      { return jsonwire.Marshal(v) }
func (v WorktreeRow) MarshalJSON() ([]byte, error)   { return jsonwire.Marshal(v) }
func (v SummaryRow) MarshalJSON() ([]byte, error)    { return jsonwire.Marshal(v) }
func (v ProcsRow) MarshalJSON() ([]byte, error)      { return jsonwire.Marshal(v) }
func (v DeleteRow) MarshalJSON() ([]byte, error)     { return jsonwire.Marshal(v) }
func (v ResnapshotRow) MarshalJSON() ([]byte, error) { return jsonwire.Marshal(v) }
func (v ProcRow) MarshalJSON() ([]byte, error)       { return jsonwire.Marshal(v) }
func (v DecodedProcs) MarshalJSON() ([]byte, error)  { return jsonwire.Marshal(v) }
