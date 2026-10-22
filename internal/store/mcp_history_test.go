package store

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMCPUnchangedMinuteWritesNoStateRows(t *testing.T) {
	s := openTest(t, "", Memory)
	at := time.Date(2026, 10, 20, 12, 0, 5, 0, time.UTC)
	sample := model.MCPObservation{Key: "server", Count: 1, Members: []model.ProcessIdentity{{PID: 1, StartedAt: at}}}
	for i := 0; i < 30; i++ {
		mustTx(t, s, func(tx *Tx) error {
			return tx.ObserveMCP(at.Add(time.Duration(i)*time.Minute), []model.MCPObservation{sample})
		})
	}
	var n int
	if err := s.conn.QueryRowContext(s.ctx, "SELECT count(*) FROM mcp_states").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("unchanged minutes wrote %d states, want 1", n)
	}
}

func TestMCPHistoryEquivalentToMinuteReference(t *testing.T) {
	st := openTest(t, "", Memory)
	at := time.Date(2026, 10, 20, 0, 0, 7, 0, time.UTC)
	var reference []model.MCPObservation
	previous := map[string]model.MCPObservation{}
	for minute := 0; minute < 24*60; minute++ {
		if minute%97 == 0 {
			continue
		} // missing observations must remain gaps
		now := at.Add(time.Duration(minute) * time.Minute)
		var observations []model.MCPObservation
		current := map[string]model.MCPObservation{}
		for group := 0; group < 3; group++ {
			if group == 1 && minute >= 400 && minute < 490 || group == 2 && minute >= 1000 {
				continue
			}
			count := 1 + (minute/71+group)%4
			row := model.MCPObservation{At: now, ResolutionSeconds: 60, Key: fmt.Sprint(group), Name: "server", Host: model.MCPHost{PID: int32(100 + group), StartedAt: at.Add(-time.Hour)}, Count: count, Footprint: uint64(1000 + minute/37)}
			for member := 0; member < count; member++ {
				start := at
				if group == 2 && member == 0 {
					start = at.Add(time.Duration(minute/113) * time.Minute)
				} // PID reuse
				if group == 0 && minute >= 900 {
					start = time.Time{}
				} // incomplete identities
				row.Members = append(row.Members, model.ProcessIdentity{PID: int32(1 + member), StartedAt: start})
			}
			// Mutable display metadata also gets interned without rewriting history.
			if minute >= 600 {
				row.Host.Cwd = "/new/workspace"
				row.Name = "renamed"
			}
			current[row.Key] = row
			observations = append(observations, row)
			reference = append(reference, row)
		}
		for key, row := range previous {
			if _, ok := current[key]; !ok && row.Count > 0 {
				row.At = now
				row.Count, row.Footprint, row.Members = 0, 0, nil
				reference = append(reference, row)
			}
		}
		previous = current
		mustTx(t, st, func(tx *Tx) error { return tx.ObserveMCP(now, observations) })
	}
	// Full window and windows cutting unchanged runs or collection gaps.
	for _, window := range [][2]int{{0, 1439}, {391, 500}, {680, 997}, {97, 98}, {900, 902}, {590, 610}, {605, 620}} {
		since, until := at.Add(time.Duration(window[0])*time.Minute), at.Add(time.Duration(window[1])*time.Minute)
		want := derive.NewMCPTrendBuilder()
		for _, row := range reference {
			if !row.At.Before(since.Truncate(time.Minute)) && !row.At.After(until) {
				want.Add(row)
			}
		}
		got := derive.NewMCPTrendBuilder()
		if err := st.ScanMCPHistory(since, until, func(row model.MCPObservation) error { got.Add(row); return nil }); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Trends(), want.Trends()) {
			t.Fatalf(`window %v:
got %+v
want %+v`, window, got.Trends(), want.Trends())
		}
	}
}

func TestMCPHistoryRetentionTiersAndBounds(t *testing.T) {
	st := openTest(t, "", Memory)
	now := time.Date(2026, 10, 22, 12, 0, 5, 0, time.UTC)
	sample := model.MCPObservation{Key: "server", Count: 1, Host: model.MCPHost{PID: 10, StartedAt: now.Add(-40 * 24 * time.Hour)}, Members: []model.ProcessIdentity{{PID: 1, StartedAt: now}}}
	// Several state changes within each hour: old hours collapse to one endpoint.
	for _, age := range []time.Duration{31 * 24 * time.Hour, 29 * 24 * time.Hour, 25 * time.Hour, 23 * time.Hour, time.Hour, 0} {
		for i := 0; i < 3; i++ {
			sample.Footprint = uint64(i)
			at := now.Add(-age + time.Duration(i)*time.Minute)
			mustTx(t, st, func(tx *Tx) error { return tx.ObserveMCP(at, []model.MCPObservation{sample}) })
		}
	}
	mustTx(t, st, func(tx *Tx) error { return tx.Prune(now.Add(2 * time.Minute)) })
	var states, hourly int
	if err := st.conn.QueryRowContext(st.ctx, `SELECT count(*),sum(resolution=3600) FROM mcp_states`).Scan(&states, &hourly); err != nil {
		t.Fatal(err)
	}
	if states != 11 || hourly != 2 {
		t.Fatalf("states=%d hourly=%d", states, hourly)
	}
	rows := mcpRows(t, st, now.Add(2*time.Minute))
	if len(rows) != 11 {
		t.Fatal(len(rows))
	}
	for _, row := range rows {
		if row.At.Before(now.Add(-24*time.Hour)) && row.ResolutionSeconds != 3600 {
			t.Fatal(row)
		}
	}
	var recent []model.MCPObservation
	err := st.ScanMCPHistory(now.Add(-24*time.Hour), now, func(row model.MCPObservation) error {
		recent = append(recent, row)
		if row.At.Before(now.Add(-24*time.Hour).Truncate(time.Minute)) || row.At.After(now) {
			t.Fatal(row)
		}
		return nil
	})
	if err != nil || len(recent) != 7 {
		t.Fatalf("recent=%d err=%v", len(recent), err)
	}
	if got := mcpRows(t, st.ReadMachine("peer"), now); len(got) != 0 {
		t.Fatal(got)
	}
	// Missing commands, including an exiting host, get one zero even on restart.
	mustTx(t, st, func(tx *Tx) error { return tx.ObserveMCP(now.Add(3*time.Minute), nil) })
	mustTx(t, st, func(tx *Tx) error { return tx.ObserveMCP(now.Add(4*time.Minute), nil) })
	var zeros int
	if err := st.conn.QueryRowContext(st.ctx, `SELECT count(*) FROM mcp_states WHERE count=0`).Scan(&zeros); err != nil || zeros != 1 {
		t.Fatalf("zeros=%d %v", zeros, err)
	}
	// Gate is durable and first successful observation wins within a minute.
	mustTx(t, st, func(tx *Tx) error {
		return tx.ObserveMCP(now.Add(4*time.Minute+time.Second), []model.MCPObservation{sample})
	})
	if err := st.ResetMachineID(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(mcpRows(t, st, now.Add(4*time.Minute))) != 12 {
		t.Fatal("history lost on identity reset")
	}
}

func TestMCPHistoryMigration19AndVacuum(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	p, err := migrationProvider(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(context.Background(), 19); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO meta VALUES('old','machine_id','old'); INSERT INTO mcp_minutes VALUES('old',1,1,1,'long command',1,'host','exe','cwd','server',1,42,'[]')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		t.Fatal(err)
	}
	oldReader, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(250)")
	if err != nil {
		t.Fatal(err)
	}
	defer oldReader.Close()
	oldSnapshot, err := oldReader.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer oldSnapshot.Rollback()
	var oldCount int
	if err := oldSnapshot.QueryRow("SELECT count(*) FROM mcp_minutes").Scan(&oldCount); err != nil {
		t.Fatal(err)
	}
	db.Close()
	st := openTest(t, path, ReadWrite)
	if err := oldSnapshot.QueryRow("SELECT count(*) FROM mcp_minutes").Scan(&oldCount); err != nil || oldCount != 1 {
		t.Fatalf("migration blocked old reader: %d %v", oldCount, err)
	}
	if err := oldSnapshot.Rollback(); err != nil {
		t.Fatal(err)
	}
	var vacuum int
	if err := st.conn.QueryRowContext(st.ctx, `PRAGMA auto_vacuum`).Scan(&vacuum); err != nil || vacuum != 2 {
		t.Fatalf("vacuum=%d %v", vacuum, err)
	}
	var old int
	if err := st.conn.QueryRowContext(st.ctx, `SELECT count(*) FROM sqlite_schema WHERE name='mcp_minutes'`).Scan(&old); err != nil || old != 0 {
		t.Fatalf("old=%d %v", old, err)
	}
	for _, table := range []string{"mcp_groups", "mcp_labels", "mcp_states", "mcp_clock"} {
		var n int
		if err := st.conn.QueryRowContext(st.ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s: %d %v", table, n, err)
		}
	}
	// Verify real page reclamation while a read-only connection keeps a snapshot.
	if err := st.exec(`CREATE TABLE vacuum_fixture(payload BLOB)`); err != nil {
		t.Fatal(err)
	}
	if err := st.exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<600) INSERT INTO vacuum_fixture SELECT zeroblob(4096) FROM n`); err != nil {
		t.Fatal(err)
	}
	reader := openTest(t, path, ReadOnly)
	snapshot, err := reader.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Rollback()
	var n int
	if err := snapshot.QueryRow(`SELECT count(*) FROM vacuum_fixture`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if err := st.exec(`DROP TABLE vacuum_fixture`); err != nil {
		t.Fatal(err)
	}
	var before, after int
	st.conn.QueryRowContext(st.ctx, `PRAGMA freelist_count`).Scan(&before)
	if err := st.ReclaimSpace(context.Background()); err != nil {
		t.Fatal(err)
	}
	st.conn.QueryRowContext(st.ctx, `PRAGMA freelist_count`).Scan(&after)
	if before <= after || before-after > 256 {
		t.Fatalf("free pages %d -> %d", before, after)
	}
	// A subsequent writable open must not repeat the full VACUUM.
	reopened := openTest(t, path, ReadWrite)
	var remaining int
	if err := reopened.conn.QueryRowContext(reopened.ctx, "PRAGMA freelist_count").Scan(&remaining); err != nil || remaining != after {
		t.Fatalf("repeated full vacuum: free pages %d -> %d (%v)", after, remaining, err)
	}
	if err := snapshot.QueryRow(`SELECT count(*) FROM vacuum_fixture`).Scan(&n); err != nil || n != 600 {
		t.Fatalf("reader snapshot: %d %v", n, err)
	}
}

func TestMCPStorageUsage(t *testing.T) {
	st := openTest(t, "", Memory)
	if err := st.exec(`CREATE TABLE large_fixture(payload BLOB); INSERT INTO large_fixture VALUES(zeroblob(100000))`); err != nil {
		t.Fatal(err)
	}
	usage, err := st.StorageUsage()
	if err != nil {
		t.Fatal(err)
	}
	if usage.LargestTable != "large_fixture" || usage.LargestTableBytes < 100000 || usage.Bytes < usage.LargestTableBytes {
		t.Fatal(usage)
	}
}

// Realistic command and identity widths, 200 groups, 1440 observations and 79%
// unchanged states. The before database uses the exact schema-19 table/index.
func TestMCPHistoryBytesPerDay(t *testing.T) {
	if testing.Short() {
		t.Skip("volume measurement")
	}
	st := openTest(t, filepath.Join(t.TempDir(), "after.db"), ReadWrite)
	before, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "before.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer before.Close()
	migration, err := migrations.ReadFile("migrations/0014_mcp_minutes.sql")
	if err != nil {
		t.Fatal(err)
	}
	ddl := strings.Split(strings.Split(string(migration), "-- +goose Up")[1], "-- +goose Down")[0]
	if _, err := before.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 20, 0, 0, 5, 0, time.UTC)
	var baseline int64
	if err := st.conn.QueryRowContext(st.ctx, `PRAGMA page_count`).Scan(&baseline); err != nil {
		t.Fatal(err)
	}
	oldTx, err := before.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer oldTx.Rollback()
	insert, err := oldTx.Prepare(`INSERT INTO mcp_minutes VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	defer insert.Close()
	err = st.Tx(context.Background(), func(tx *Tx) error {
		samples := make([]model.MCPObservation, 200)
		for group := range samples {
			samples[group] = model.MCPObservation{Key: fmt.Sprintf("command:%04d:%s", group, strings.Repeat("x", 665)), Name: "server", Host: model.MCPHost{PID: int32(group + 100), StartedAt: at.Add(-time.Hour), Name: "claude", Exe: "/Applications/Claude.app/Contents/MacOS/claude", Cwd: "/Users/fixture/projects/workspace"}, Count: 2, Footprint: 100000000, Members: []model.ProcessIdentity{{PID: int32(group*2 + 1000), StartedAt: at}, {PID: int32(group*2 + 1001), StartedAt: at}}}
		}
		for minute := 0; minute < 1440; minute++ {
			now := at.Add(time.Duration(minute) * time.Minute)
			for group := range samples {
				if (minute*37+group*17)%100 < 21 {
					samples[group].Footprint++
				}
				row := samples[group]
				members := fmt.Sprintf(`[{"pid":%d,"started_at":"2026-10-20T00:00:05Z"},{"pid":%d,"started_at":"2026-10-20T00:00:05Z"}]`, row.Members[0].PID, row.Members[1].PID)
				if _, err := insert.Exec(tx.machineID, unix(now.Truncate(time.Minute)), row.Host.PID, unix(row.Host.StartedAt), row.Key, unix(now), row.Host.Name, row.Host.Exe, row.Host.Cwd, row.Name, row.Count, row.Footprint, members); err != nil {
					return err
				}
			}
			if err := tx.ObserveMCP(now, samples); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := oldTx.Commit(); err != nil {
		t.Fatal(err)
	}
	var oldPages, newPages, pageSize, states int64
	if err := before.QueryRow(`PRAGMA page_count`).Scan(&oldPages); err != nil {
		t.Fatal(err)
	}
	st.conn.QueryRowContext(st.ctx, `PRAGMA page_count`).Scan(&newPages)
	st.conn.QueryRowContext(st.ctx, `PRAGMA page_size`).Scan(&pageSize)
	st.conn.QueryRowContext(st.ctx, `SELECT count(*) FROM mcp_states`).Scan(&states)
	oldBytes, newBytes := oldPages*pageSize, (newPages-baseline)*pageSize
	t.Logf("200 groups x 1440 minutes: before=%d bytes/day after=%d bytes/day states=%d reduction=%.2fx", oldBytes, newBytes, states, float64(oldBytes)/float64(newBytes))
	if newBytes > oldBytes/3 || states > 70000 {
		t.Fatalf("storage growth: before=%d after=%d states=%d", oldBytes, newBytes, states)
	}
}

func mcpRows(t *testing.T, s interface {
	ScanMCPHistory(time.Time, time.Time, func(model.MCPObservation) error) error
}, at time.Time) []model.MCPObservation {
	t.Helper()
	var rows []model.MCPObservation
	if err := s.ScanMCPHistory(at.Add(-30*24*time.Hour), at, func(s model.MCPObservation) error { rows = append(rows, s); return nil }); err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestMCPPruningRebasesObservedWindow(t *testing.T) {
	s := openTest(t, "", Memory)
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	ps := []model.Proc{{PID: 100, Cmdline: "claude", StartTime: at.Add(-time.Hour * 24)}}
	for i, minutes := range []int{-61, -60, -59, -30, 0} {
		now := at.Add(time.Duration(minutes) * time.Minute)
		if i == 1 {
			ps = ps[:1]
		} // an end outside the snapshot window
		ps = append(ps, model.Proc{PID: int32(i + 1), PPID: 100, Cmdline: "node /opt/server.js", StartTime: now})
		v := derive.MCPServers(ps, nil, now)
		mustTx(t, s, func(tx *Tx) error { return tx.ObserveMCP(now, v.Samples) })
	}
	trends := func() []derive.MCPTrend {
		b := derive.NewMCPTrendBuilder()
		for _, row := range mcpRows(t, s, at) {
			b.Add(row)
		}
		return b.Trends()
	}
	if got := trends()[0]; got.State != "churning" || got.EndedCount != 1 {
		t.Fatal(got)
	}
	mustTx(t, s, func(tx *Tx) error { return tx.PruneSnapshot(at) })
	got := trends()[0]
	if got.State != "leaking" || got.StartedCount != 2 || got.EndedCount != 0 || !got.FirstSample.Equal(at.Add(-59*time.Minute)) || got.RatePerHour != 2/(59.0/60) {
		t.Fatal(got)
	}
}

func TestMCPHistoryBoundsBeforeDecoding(t *testing.T) {
	st := openTest(t, "", Memory)
	now := time.Date(2026, 10, 22, 12, 0, 5, 0, time.UTC)
	row := model.MCPObservation{Key: "server", Count: 1}
	for _, at := range []time.Time{now.Add(-25 * time.Hour), now, now.Add(time.Minute)} {
		row.Footprint++
		mustTx(t, st, func(tx *Tx) error { return tx.ObserveMCP(at, []model.MCPObservation{row}) })
	}
	if err := st.exec(`UPDATE mcp_states SET members='invalid json' WHERE first_at<? OR first_at>?`, unix(now.Add(-24*time.Hour)), unix(now)); err != nil {
		t.Fatal(err)
	}
	count := 0
	if err := st.ScanMCPHistory(now.Add(-24*time.Hour), now, func(model.MCPObservation) error { count++; return nil }); err != nil || count != 1 {
		t.Fatalf("out-of-window rows decoded: count=%d %v", count, err)
	}
	// Prove the corrupt evidence is present and would fail a broader scan.
	if err := st.ScanMCPHistory(now.Add(-30*24*time.Hour), now, func(model.MCPObservation) error { return nil }); err == nil {
		t.Fatal("fixture did not contain old corrupt evidence")
	}
}

func TestMCPHeartbeatAndMembershipOrder(t *testing.T) {
	st := openTest(t, "", Memory)
	at := time.Date(2026, 10, 22, 12, 59, 5, 0, time.UTC)
	row := model.MCPObservation{Key: "server", Count: 2, Members: []model.ProcessIdentity{{PID: 1, StartedAt: at}, {PID: 2, StartedAt: at}}}
	for i := 0; i < 3; i++ {
		row.Members[0], row.Members[1] = row.Members[1], row.Members[0]
		mustTx(t, st, func(tx *Tx) error {
			return tx.ObserveMCP(at.Add(time.Duration(i)*time.Minute), []model.MCPObservation{row})
		})
	}
	var states int
	if err := st.conn.QueryRowContext(st.ctx, `SELECT count(*) FROM mcp_states`).Scan(&states); err != nil || states != 2 {
		t.Fatalf("hourly heartbeat or membership order: %d %v", states, err)
	}
	rows := mcpRows(t, st, at.Add(2*time.Minute))
	if !rows[len(rows)-1].At.Equal(at.Add(2 * time.Minute)) {
		t.Fatal("heartbeat endpoint stale", rows)
	}
}
