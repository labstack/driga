package store

import (
	"context"
	"database/sql"
	"github.com/pressly/goose/v3"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonMigrationRewritesPersistedContracts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	sub, _ := fs.Sub(migrations, "migrations")
	p, err := goose.NewProvider(goose.DialectSQLite3, db, sub, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.UpTo(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO meta VALUES('old','machine_id','old');
 INSERT INTO sessions (machine_id,adapter,external_id,origin,version,pid,proc_start,cwd,branch,title,last_prompt,registry_status,state,state_heuristic,evidence,started_at,last_activity_at,hosted,turn_open,source_path) VALUES('old','claude','s','','',0,0,'','','','','','~stalled?',0,'',0,0,0,0,'');
 INSERT INTO inbox_items(machine_id,id,session_id,kind,ts,summary) VALUES('old','a',1,'worker-finished',1,'preserve same-worktree prose');
 INSERT INTO feed(seq,ts,kind,key,payload) VALUES(7,1,'session','key','{"label":"~stalled?","heuristic":false,"started_at":"2026-10-19T12:00:00.123456789-07:00","completions":[{"ts":"2026-10-19T12:00:00-07:00"}],"summary":"preserve same-worktree prose","rss":9007199254740993}');
 INSERT INTO feed(seq,ts,kind,key,payload) VALUES(8,1,'machine_sample','sample','{"ts":"2026-10-19T12:00:00-07:00","mem_used":17}');`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	st, err := Open(path, ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sessions, err := st.Sessions(SessionFilter{})
	if err != nil || len(sessions) != 1 || sessions[0].Label != "stalled" || !sessions[0].Heuristic {
		t.Fatalf("migrated sessions %+v %v", sessions, err)
	}
	var kind, payload string
	if err = st.db.QueryRow("SELECT kind FROM inbox_items").Scan(&kind); err != nil || kind != "worker_finished" {
		t.Fatal(kind, err)
	}
	if err = st.db.QueryRow("SELECT payload FROM feed WHERE seq=7").Scan(&payload); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"label":"stalled"`, `"heuristic":true`, `"occurred_at"`, `"rss_bytes":9007199254740993`, `2026-10-19T19:00:00.123456789Z`, `preserve same-worktree prose`} {
		if !strings.Contains(payload, want) {
			t.Errorf("missing %s in %s", want, payload)
		}
	}
	if err = st.db.QueryRow("SELECT payload FROM feed WHERE seq=8").Scan(&payload); err != nil || !strings.Contains(payload, `"sampled_at"`) || !strings.Contains(payload, `"mem_used_bytes":17`) {
		t.Fatal(payload, err)
	}
}
