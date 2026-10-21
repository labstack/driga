package flowtest

import (
	"fmt"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

// SeedAmbiguousAttention creates distinct sessions with the same project/reason.
func SeedAmbiguousAttention(tx *store.Tx) error {
	repo, err := tx.UpsertRepo(model.Repo{Name: "sample", Path: "/sample"})
	if err != nil {
		return err
	}
	for i, obs := range []model.SessionObs{
		{ExternalID: "session-aaa111", Title: "Fix cache", Branch: "work/cache"},
		{ExternalID: "session-bbb222", Title: "Fix parser", Branch: "work/parser"},
		{ExternalID: "session-ccc333"},
		{ExternalID: "session-ddd444"},
	} {
		obs.Adapter, obs.PID, obs.StartedAt = "claude", int32(i+1), Now.Add(-time.Hour)
		id, err := tx.UpsertSession(model.Session{RepoID: &repo, SessionObs: obs, Label: "waiting"})
		if err != nil {
			return err
		}
		if err := tx.UpsertInboxItem(model.InboxItem{ID: fmt.Sprint("ambiguous-", i), SessionID: id, Kind: "waiting", Reason: "unknown", TS: Now.Add(-time.Minute)}); err != nil {
			return err
		}
	}
	return nil
}
