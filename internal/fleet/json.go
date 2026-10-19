package fleet

import "github.com/labstack/driga/internal/jsonwire"

func (v Peer) MarshalJSON() ([]byte, error)          { return jsonwire.Marshal(v) }
func (v Viewer) MarshalJSON() ([]byte, error)        { return jsonwire.Marshal(v) }
func (v Snapshot) MarshalJSON() ([]byte, error)      { return jsonwire.Marshal(v) }
func (v SnapshotFresh) MarshalJSON() ([]byte, error) { return jsonwire.Marshal(v) }
func (v snapshotFile) MarshalJSON() ([]byte, error)  { return jsonwire.Marshal(v) }
