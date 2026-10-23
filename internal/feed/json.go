package feed

import "github.com/labstack/driga/internal/jsonx"

func (v FeedRow) MarshalJSON() ([]byte, error) { type plain FeedRow; return jsonx.Marshal(plain(v)) }
func (v SessionKey) MarshalJSON() ([]byte, error) {
	type plain SessionKey
	return jsonx.Marshal(plain(v))
}
func (v SessionRow) MarshalJSON() ([]byte, error) {
	return jsonx.Object(v.Session, struct {
		Parent       *SessionKey `json:"parent,omitzero"`
		RepoPath     string      `json:"repo_path,omitempty"`
		WorktreePath string      `json:"worktree_path,omitempty"`
	}{v.Parent, v.RepoPath, v.WorktreePath})
}
func (v FilesRow) MarshalJSON() ([]byte, error) { type plain FilesRow; return jsonx.Marshal(plain(v)) }
func (v HandoffRow) MarshalJSON() ([]byte, error) {
	type plain HandoffRow
	return jsonx.Marshal(plain(v))
}
func (v InboxRow) MarshalJSON() ([]byte, error) {
	return jsonx.Object(v.InboxItem, struct {
		Session SessionKey `json:"session"`
	}{v.Session})
}
func (v WorktreeRow) MarshalJSON() ([]byte, error) {
	return jsonx.Object(v.Worktree, struct {
		RepoPath string `json:"repo_path"`
	}{v.RepoPath})
}
func (v SummaryRow) MarshalJSON() ([]byte, error) {
	return jsonx.Object(v.Summary, struct {
		RepoPath string `json:"repo_path"`
	}{v.RepoPath})
}
func (v ProcsRow) MarshalJSON() ([]byte, error) { type plain ProcsRow; return jsonx.Marshal(plain(v)) }
func (v DeleteRow) MarshalJSON() ([]byte, error) {
	type plain DeleteRow
	return jsonx.Marshal(plain(v))
}
func (v ResnapshotRow) MarshalJSON() ([]byte, error) {
	type plain ResnapshotRow
	return jsonx.Marshal(plain(v))
}
func (v ProcRow) MarshalJSON() ([]byte, error) {
	return jsonx.Object(v.Proc, struct {
		Session *SessionKey `json:"session,omitzero"`
	}{v.Session})
}
func (v DecodedProcs) MarshalJSON() ([]byte, error) {
	type plain DecodedProcs
	return jsonx.Marshal(plain(v))
}
