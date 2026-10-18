package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestSessionLastPromptAtRoundTrip(t *testing.T) {
	s := openTest(t, filepath.Join(t.TempDir(), "driga.db"), ReadWrite)
	at := time.Unix(1700000000, 42)
	var id int64
	mustTx(t, s, func(tx *Tx) (err error) {
		id, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "x", LastPromptAt: at}})
		return err
	})
	got, err := s.Session(id)
	if err != nil || !got.LastPromptAt.Equal(at) {
		t.Fatalf("LastPromptAt = %v, %v; want %v", got.LastPromptAt, err, at)
	}
}
