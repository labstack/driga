package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/store"
)

func TestDoctorQuietEventSources(t *testing.T) {
	d, st := doctorFixture(t)
	for _, name := range []string{"sockets", "otlp"} {
		if err := st.Tx(context.Background(), func(tx *store.Tx) error { return tx.SetSourceOK(name, d.Now.Add(-12*time.Minute)) }); err != nil {
			t.Fatal(err)
		}
	}
	c := requireCheck(t, Doctor(context.Background(), d), "source:sockets", "ok", "")
	if c.Fix != "" || !strings.Contains(c.Detail, "last event") {
		t.Fatal(c)
	}
	if err := st.Tx(context.Background(), func(tx *store.Tx) error { return tx.SetSourceError("sockets", "probe failed", d.Now) }); err != nil {
		t.Fatal(err)
	}
	requireCheck(t, Doctor(context.Background(), d), "source:sockets", "warn", "driga service restart")
}
