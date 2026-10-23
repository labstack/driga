package fleet

import "github.com/labstack/driga/internal/jsonx"

func (v Peer) MarshalJSON() ([]byte, error)     { type plain Peer; return jsonx.Marshal(plain(v)) }
func (v Viewer) MarshalJSON() ([]byte, error)   { type plain Viewer; return jsonx.Marshal(plain(v)) }
func (v Snapshot) MarshalJSON() ([]byte, error) { type plain Snapshot; return jsonx.Marshal(plain(v)) }
func (v SnapshotFresh) MarshalJSON() ([]byte, error) {
	type plain SnapshotFresh
	return jsonx.Marshal(plain(v))
}
func (v snapshotFile) MarshalJSON() ([]byte, error) {
	return jsonx.Object(v.FileTouch, struct {
		SessionID int64 `json:"session_id"`
	}{v.SessionID})
}
