package store

import (
	"context"
	"strings"
	"testing"
)

func TestActiveSessionsUseActivityIndex(t *testing.T) {
	st := openTest(t, "", Memory)
	rows, err := st.db.Query("EXPLAIN QUERY PLAN SELECT id,"+sessionColumns+" FROM sessions WHERE machine_id=? AND ended_at IS NULL ORDER BY last_activity_at DESC,id", st.MachineID())
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	text := strings.Join(plan, "; ")
	t.Log(text)
	if !strings.Contains(text, "sessions_active_activity") || strings.Contains(text, "TEMP B-TREE") {
		t.Fatal(text)
	}
}
func BenchmarkActiveSessions(b *testing.B) {
	for _, indexed := range []bool{false, true} {
		name := "before"
		if indexed {
			name = "indexed"
		}
		b.Run(name, func(b *testing.B) {
			st, err := Open("", Memory)
			if err != nil {
				b.Fatal(err)
			}
			defer st.Close()
			// Populate the exact schema using one recursive insert; only ended_at differs.
			_, err = st.db.Exec(`WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<101000)
 INSERT INTO sessions(machine_id,adapter,external_id,origin,version,pid,proc_start,cwd,branch,title,last_prompt,registry_status,state,state_heuristic,evidence,started_at,last_activity_at,hosted,turn_open,ended_at,source_path)
 SELECT ?,'claude',CAST(i AS TEXT),'cli','',0,0,'/synthetic','','Synthetic','','alive','working',0,'',1,i,0,0,CASE WHEN i<=100000 THEN 1 ELSE NULL END,'' FROM n`, st.MachineID())
			if err != nil {
				b.Fatal(err)
			}
			if !indexed {
				if _, err = st.db.Exec("DROP INDEX sessions_active_activity"); err != nil {
					b.Fatal(err)
				}
			}
			rows, err := st.db.Query("EXPLAIN QUERY PLAN SELECT id,"+sessionColumns+" FROM sessions WHERE machine_id=? AND ended_at IS NULL ORDER BY last_activity_at DESC,id", st.MachineID())
			if err != nil {
				b.Fatal(err)
			}
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
					b.Fatal(err)
				}
				b.Log(detail)
			}
			rows.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err = st.Sessions(SessionFilter{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
func TestForwardMigrationAddsActivityIndex(t *testing.T) {
	st := openTest(t, "", Memory)
	// Rewind to the baseline, then run goose forward.
	for _, sql := range []string{"ALTER TABLE sessions DROP COLUMN conversation_claimed", "ALTER TABLE messages DROP COLUMN decided_by", "ALTER TABLE messages DROP COLUMN tool", "DROP INDEX sessions_active_activity", "ALTER TABLE procs DROP COLUMN argv", "DROP TABLE panel_events", "DROP TABLE panel_polls", "DROP TABLE prompt_intervals", "ALTER TABLE machine_samples DROP COLUMN disk_total", "ALTER TABLE machine_samples DROP COLUMN cpu_percent", "DELETE FROM goose_db_version WHERE version_id>1"} {
		if _, err := st.db.Exec(sql); err != nil {
			t.Fatal(err)
		}
	}
	provider, err := migrationProvider(st.db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = st.db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name='sessions_active_activity'").Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}
