package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestAgentsStartedFrom(t *testing.T) {
	readEnv(t)
	readDB(t, func(tx *store.Tx) error {
		_, err := tx.UpsertSession(model.Session{StartedFrom: "Fixture Editor", SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "surface", Title: "Task", LastActivityAt: time.Now()}, Label: "working"})
		return err
	})
	for _, command := range []string{"agents"} {
		out, _ := readRun(t, command)
		if !strings.Contains(out, "Task · from Fixture Editor") {
			t.Fatal(command, out)
		}
	}
}
