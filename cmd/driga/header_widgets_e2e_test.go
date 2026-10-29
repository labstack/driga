package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/labstack/driga/internal/dashboard"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/panel"
	"github.com/labstack/driga/internal/store"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Reuse the built-binary smoke test's isolated node and bounded polling.
// A seeded daemon database supplies deterministic readings; no agent CLI or
// collector is needed. The test process stands in for the daemon lock owner.
func TestHeaderEditReachesApp(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the driga binary")
	}
	if runtime.GOOS == "windows" {
		t.Skip("uses the Unix isolated smoke-test node")
	}
	bin := filepath.Join(t.TempDir(), "bin", "driga")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	n := newNode(t, bin, "header")
	write(t, n.path("config.toml"), "no_service_hint = true\n[fleet]\nserve = false\n[intake]\notlp = false\n[adapters.claude]\nenabled = false\n[adapters.codex]\nenabled = false\n[adapters.copilot]\nenabled = false\n[adapters.gemini]\nenabled = false\n")
	st, err := store.Open(n.path("data", "driga.db"), store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	cpu := 42.0
	err = st.Tx(context.Background(), func(tx *store.Tx) error {
		if err := tx.SetMeta("last_pass_at", now.Format(time.RFC3339Nano)); err != nil {
			return err
		}
		if err := tx.UpsertMachine(model.Machine{Name: "header-fixture", Hostname: "header-fixture"}); err != nil {
			return err
		}
		return tx.InsertSample(model.MachineSample{TS: now, MemUsed: 24 << 30, MemTotal: 32 << 30, CPUPercent: &cpu})
	})
	closeErr := st.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("seed: %v %v", err, closeErr)
	}
	write(t, n.path("data", "driga.lock"), fmt.Sprintf(`{"pid":%d,"kind":"manual","executable":"fixture","version":"test"}`, os.Getpid()))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var logs logBuffer
	serve := n.cmd(ctx, "dev", "serve", "--addr", addr)
	serve.Stdout, serve.Stderr = &logs, &logs
	if err := serve.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = serve.Wait() }()
	client := &http.Client{Timeout: 2 * time.Second}
	open := func() (panel.Fragment, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/app/api/open", bytes.NewBufferString(`{"id":"header"}`))
		if err != nil {
			return panel.Fragment{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://"+addr)
		response, err := client.Do(req)
		if err != nil {
			return panel.Fragment{}, err
		}
		defer response.Body.Close()
		var f panel.Fragment
		if response.StatusCode != 200 {
			return f, fmt.Errorf("open: HTTP %d", response.StatusCode)
		}
		err = json.NewDecoder(response.Body).Decode(&f)
		return f, err
	}
	eventually(t, 20*time.Second, func() bool { f, err := open(); return err == nil && f.ID == "header" && f.Version == 0 }, logs.String)
	cmd := n.cmd(ctx, "mcp")
	cmd.Stderr = &logs
	session, err := sdk.NewClient(&sdk.Implementation{Name: "header-e2e", Version: "test"}, nil).Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("MCP connect: %v\n%s", err, logs.String())
	}
	defer session.Close()
	catalog := panel.HeaderCatalog()
	cpuPanel := catalog[slices.IndexFunc(catalog, func(p panel.Panel) bool { return p.ID == "cpu" })]
	result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "edit_dashboard", Arguments: map[string]any{
		"id": "header", "base_version": 0, "message": "Add CPU and remove Reclaimable",
		"operations": []map[string]any{{"op": "add", "panel": cpuPanel}, {"op": "remove", "id": "reclaim"}},
	}})
	if err != nil || result.IsError {
		t.Fatalf("MCP edit: %+v %v\n%s", result, err, logs.String())
	}
	// One normal app refresh interval, then request exactly what the app polls.
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	f, err := open()
	if err != nil {
		t.Fatalf("refresh: %v\n%s", err, logs.String())
	}
	ids := make([]string, len(f.Dashboard.Panels))
	for i, p := range f.Dashboard.Panels {
		ids[i] = p.ID
	}
	if f.Version != 1 || !reflect.DeepEqual(ids, []string{"waiting", "finished", "stuck", "leftover", "memory", "cpu"}) {
		t.Fatalf("app did not reflect MCP edit: %+v", f)
	}
	r := f.Results[slices.IndexFunc(f.Results, func(r panel.Result) bool { return r.ID == "cpu" })]
	if r.Status != panel.StatusOK || r.Frame == nil || len(r.Frame.Rows) != 1 || r.Frame.Rows[0][0] != cpu {
		t.Fatalf("CPU reading: %+v", r)
	}
	versions, err := dashboard.New(n.path("data")).Versions("header")
	if err != nil || len(versions) != 1 || versions[0].Author.Kind != "agent" {
		t.Fatalf("MCP authorship: %+v %v", versions, err)
	}
}
