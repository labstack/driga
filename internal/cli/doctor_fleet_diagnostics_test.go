package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestDoctorSocketPathsAndFleetErrors(t *testing.T) {
	d, st := doctorFixture(t)
	const detail = "fleet /v1/snapshot: 503 Service Unavailable: snapshot unavailable: invalid uri authority: a"
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		if err := tx.SetSourceError("chat", "chat socket path is not a socket", d.Now); err != nil {
			return err
		}
		return tx.ForMachine("remote").UpsertMachine(model.Machine{Name: "peer-a", LastError: detail})
	}); err != nil {
		t.Fatal(err)
	}
	checks := Doctor(context.Background(), d)
	c := requireCheck(t, checks, "chat socket", "warn", "")
	if !strings.Contains(c.Detail, chat.SocketPath(d.DataDir)) || !strings.Contains(c.Detail, "not a socket") {
		t.Fatal(c)
	}
	c = requireCheck(t, checks, "fleet socket", "info", "")
	if !strings.Contains(c.Detail, fleet.ControlSocketPath(d.DataDir)) {
		t.Fatal(c)
	}
	c = requireCheck(t, checks, "fleet:peer-a", "warn", "")
	if !strings.Contains(c.Detail, detail) {
		t.Fatal(c)
	}
}
