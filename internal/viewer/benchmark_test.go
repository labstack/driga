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

func BenchmarkViewerLoad(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			st, err := store.Open("", store.Memory)
			if err != nil {
				b.Fatal(err)
			}
			defer st.Close()
			now := time.Now()
			err = st.Tx(context.Background(), func(tx *store.Tx) error {
				for i := 0; i < n; i++ {
					_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: fmt.Sprint(i), Cwd: "/synthetic", StartedAt: now, LastActivityAt: now, Title: "Synthetic session"}, Label: "working"})
					if err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				b.Fatal(err)
			}
			src := NewStoreSource(st, b.TempDir(), view.Filter{})
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := src.Load(context.Background()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
