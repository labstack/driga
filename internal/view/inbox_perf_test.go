package view

import (
	"context"
	"fmt"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"testing"
	"time"
)

type emptyInboxReader struct {
	Reader
	sessions, repos int
}

func (r *emptyInboxReader) Inbox(time.Time, bool) ([]model.InboxItem, error) {
	return []model.InboxItem{}, nil
}
func (r *emptyInboxReader) Sessions(store.SessionFilter) ([]model.Session, error) {
	r.sessions++
	return nil, nil
}
func (r *emptyInboxReader) Repos() ([]model.Repo, error) { r.repos++; return nil, nil }
func TestEmptyInboxSkipsSessionMaterialization(t *testing.T) {
	r := &emptyInboxReader{}
	v, err := inboxLocal(r, Filter{}, time.Now())
	if err != nil || v.Rows == nil || len(v.Rows) != 0 || r.sessions != 0 || r.repos != 0 {
		t.Fatalf("rows=%v sessions=%d repos=%d err=%v", v.Rows, r.sessions, r.repos, err)
	}
}
func BenchmarkEmptyInboxRetainedSessions(b *testing.B) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		b.Fatal(err)
	}
	defer st.Close()
	now := time.Now()
	err = st.Tx(context.Background(), func(tx *store.Tx) error {
		for i := 0; i < 100000; i++ {
			_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: fmt.Sprint(i), StartedAt: now, LastActivityAt: now}, EndedAt: &now})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := inboxLocal(st, Filter{}, now); err != nil {
			b.Fatal(err)
		}
	}
}
