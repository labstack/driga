package store

import "github.com/labstack/driga/internal/model"

func (tx *Tx) UpsertSummary(s model.Summary) error {
	return tx.exec(`INSERT INTO summaries(machine_id,project_id,input_hash,state_hash,text,model,used_snippets,created_at)
 SELECT ?,id,?,?,?,?,?,? FROM repos WHERE machine_id=? AND id=?
 ON CONFLICT(machine_id,project_id,used_snippets) DO UPDATE SET
 input_hash=excluded.input_hash,state_hash=excluded.state_hash,text=excluded.text,model=excluded.model,created_at=excluded.created_at
 WHERE excluded.created_at>=summaries.created_at`, tx.machineID, s.InputHash, s.StateHash, s.Text, s.Model, s.UsedSnippets, unix(s.CreatedAt), tx.machineID, s.ProjectID)
}

// Summaries returns local-to-this-reader project variants, newest first.
func (q *querier) Summaries(projectID int64) ([]model.Summary, error) {
	return readMany(q, `SELECT machine_id,project_id,input_hash,state_hash,text,model,used_snippets,created_at
 FROM summaries WHERE machine_id=? AND (?=0 OR project_id=?) ORDER BY created_at DESC,used_snippets DESC,project_id`, []any{q.machineID, projectID, projectID}, func(s scanner) (model.Summary, error) {
		var row model.Summary
		var at int64
		err := s.Scan(&row.MachineID, &row.ProjectID, &row.InputHash, &row.StateHash, &row.Text, &row.Model, &row.UsedSnippets, &at)
		row.CreatedAt = fromUnix(at)
		return row, err
	})
}
