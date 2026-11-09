package actions

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/store"
)

type ownershipPlatform struct {
	platform.Platform // Unimplemented operations cannot touch native processes.
	procs             []model.Proc
	cwds              map[int32]string
}

func (p ownershipPlatform) Procs(context.Context) ([]model.Proc, error) {
	return append([]model.Proc(nil), p.procs...), nil
}
func (p ownershipPlatform) Cwds(context.Context, []int32) (map[int32]string, error) {
	return p.cwds, nil
}

func TestOrphanOwnershipReloadsSessionsAndNativeDirectories(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "driga.db"), store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	g := Environment{OpenReader: func(context.Context) (*store.Store, string, func(), error) { return st, "", func() {}, nil }}
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	p := ownershipPlatform{procs: []model.Proc{{PID: 10, PPID: 1, StartTime: start}, {PID: 90, StartTime: start}}, cwds: map[int32]string{10: "/repo"}}
	o := model.Orphan{ID: "10:1", RootPID: 10, StartTime: start, Cwd: "/old-displayed-dir", Verified: true}
	if err := verifyOrphanOwnership(ctx, g, p, o); err != nil {
		t.Fatal(err)
	}
	// A session appeared after the candidate was displayed. Even a previously
	// verified idle tree must not be automatically stopped in the same directory.
	err = st.Tx(ctx, func(tx *store.Tx) error {
		_, err := tx.UpsertSession(model.Session{Label: "working", SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "new-session", PID: 90, ProcStart: start, Cwd: "/repo"}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyOrphanOwnership(ctx, g, p, o); err == nil || !strings.Contains(err.Error(), "ownership requires review") {
		t.Fatalf("fresh live session ignored: %v", err)
	}
	// A failed directory observation must not turn the same overlap into proof
	// that this tree is disconnected.
	p.cwds = nil
	if err := verifyOrphanOwnership(ctx, g, p, o); err == nil {
		t.Fatal("missing current cwd authorized kill")
	}
}
