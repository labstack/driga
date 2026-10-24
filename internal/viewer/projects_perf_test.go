package viewer

import (
	"context"
	"fmt"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
	"testing"
	"time"
)

func projectSource(t testing.TB, n int) (*StoreSource, int64) {
	t.Helper()
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	now := time.Now()
	var first int64
	err = st.Tx(context.Background(), func(tx *store.Tx) error {
		for i := 0; i < n; i++ {
			path := fmt.Sprintf("/synthetic/project-%d", i)
			id, err := tx.UpsertRepo(model.Repo{Path: path, Name: fmt.Sprint(i)})
			if err != nil {
				return err
			}
			if i == 0 {
				first = id
			}
			for j := 0; j < 10; j++ {
				_, err = tx.UpsertSession(model.Session{RepoID: &id, SessionObs: model.SessionObs{Adapter: "claude", ExternalID: fmt.Sprintf("%d-%d", i, j), Cwd: path, StartedAt: now, LastActivityAt: now}, Label: "working"})
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return NewStoreSource(st, t.TempDir(), view.Filter{}), first
}
func TestOverviewDefersProjectDetailsToSelection(t *testing.T) {
	src, id := projectSource(t, 3)
	snap, err := src.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Projects.Rows) != 3 || len(snap.ProjectDetails) != 0 {
		t.Fatalf("projects=%d eager details=%d", len(snap.Projects.Rows), len(snap.ProjectDetails))
	}
	detail, err := src.LoadProject(context.Background(), store.Scope{}, id, false)
	if err != nil || detail.Status.Repo.ID != id || len(detail.Agents.Rows) != 10 {
		t.Fatalf("selected detail: %+v %v", detail, err)
	}
}
func BenchmarkOverviewProjects(b *testing.B) {
	src, id := projectSource(b, 100)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := src.Load(context.Background()); err != nil {
			b.Fatal(err)
		}
		if _, err := src.LoadProject(context.Background(), store.Scope{}, id, false); err != nil {
			b.Fatal(err)
		}
	}
}
