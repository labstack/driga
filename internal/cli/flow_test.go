package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/labstack/driga/internal/flowtest"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

func TestFlowSharedDefaultJSON(t *testing.T) {
	readEnv(t)
	var want int64
	readDB(t, func(tx *store.Tx) (err error) { want, err = flowtest.SeedDefault(tx); return err })
	var out, diagnostic bytes.Buffer
	// Cached mode exercises the real command/read/render path with fixture data
	// without probing a collector PID or collecting this machine's processes.
	if err := (FlowCmd{}).Run(&Globals{JSON: true, Machine: "all", cachedMachines: true, Stdout: &out, Stderr: &diagnostic}); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Data view.FlowView `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Data.Lead == nil || got.Data.Lead.ID != want {
		t.Fatalf("flow --json lead = %+v; want %d", got.Data.Lead, want)
	}
}
