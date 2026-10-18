package view

import (
	"bytes"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestOrphansRenderVerifiedAndSnapshotFooter(t *testing.T) {
	st := fixture(t, func(tx *store.Tx) error {
		return tx.ReplaceOrphans([]model.Orphan{{ID: "2:1", RootPID: 2, Verified: true}})
	})
	v, err := Orphans(st, Filter{}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Render(&out, v, RenderOpts{NoColor: true, Envelope: Envelope{Mode: "snapshot"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "VERIFIED") || !strings.Contains(out.String(), "unverified (snapshot)") {
		t.Fatal(out.String())
	}
	out.Reset()
	if err := Render(&out, v, RenderOpts{JSON: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"verified": true`) {
		t.Fatal(out.String())
	}
}
