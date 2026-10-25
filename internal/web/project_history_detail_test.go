package web

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestProjectDetailAll(t *testing.T) {
	s, st, _, project := fixture(t)
	err := st.Tx(context.Background(), func(tx *store.Tx) error {
		_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: "hosted", Title: "Hosted old work", Hosted: true, LastActivityAt: time.Now().Add(-2 * time.Hour)}, RepoID: &project, Label: "idle"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, all := range []bool{false, true} {
		path := fmt.Sprint("/projects/", project)
		if all {
			path += "?all=1"
		}
		w := request(s, "GET", path)
		if w.Code != 200 || strings.Contains(w.Body.String(), "Hosted old work") != all {
			t.Fatalf("all=%v: %d %s", all, w.Code, w.Body)
		}
	}
}
