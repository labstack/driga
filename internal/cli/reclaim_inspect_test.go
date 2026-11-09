package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/reclaim"
	"github.com/labstack/driga/internal/store"
)

type reclaimReviewPlatform struct {
	platform.Platform // Unimplemented operations must never reach the host.
	procs             []model.Proc
	procsCalls        int
	peerCalls         [][]int32
	cwdErr            error
}

func (*reclaimReviewPlatform) ProcessOS() string { return "linux" }
func (p *reclaimReviewPlatform) Procs(context.Context) ([]model.Proc, error) {
	p.procsCalls++
	return append([]model.Proc{}, p.procs...), nil
}
func (p *reclaimReviewPlatform) Cwds(_ context.Context, pids []int32) (map[int32]string, error) {
	result := map[int32]string{}
	for _, process := range p.procs {
		result[process.PID] = process.Cwd
	}
	return result, p.cwdErr
}
func (p *reclaimReviewPlatform) ExternalPeers(_ context.Context, pids []int32) (map[int32]model.PeerState, error) {
	p.peerCalls = append(p.peerCalls, append([]int32{}, pids...))
	result := map[int32]model.PeerState{}
	for _, pid := range pids {
		result[pid] = model.PeerNone
	}
	return result, nil
}

func TestReclaimExactPIDWithoutCollectorOrDatabase(t *testing.T) {
	readEnv(t)
	start := time.Now().Add(-72 * time.Hour)
	p := &reclaimReviewPlatform{procs: []model.Proc{{PID: int32(os.Getpid()), OwnerKnown: true, OwnerUID: 123, StartTime: start, Exe: "/tools/driga"}}}
	basePID := int32(os.Getpid()) + 10000
	for pid := basePID; pid < basePID+100; pid++ {
		p.procs = append(p.procs, model.Proc{PID: pid, PPID: 1, OwnerKnown: true, OwnerUID: 123, StartTime: start, Exe: "/bin/bash", Argv: []string{"bash", "/tmp/test/lifecycle.sh"}, Cmdline: "bash /tmp/test/lifecycle.sh", Cwd: t.TempDir(), RSS: 100, MemoryMeasure: "rss"})
	}
	pid := basePID + 99
	var out bytes.Buffer
	g := &Globals{JSON: true, Stdout: &out, CleanupPlatform: p}
	if err := (ReclaimListCmd{PID: &pid}).Run(g); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Schema int
		Data   []reclaim.Opportunity
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var target *reclaim.Opportunity
	for i := range envelope.Data {
		if envelope.Data[i].RootPID == pid {
			target = &envelope.Data[i]
		}
	}
	if envelope.Schema != 2 || target == nil || target.Name != "lifecycle.sh" || target.Status != "review" || target.CPUKnown == nil || *target.CPUKnown {
		t.Fatalf("exact PID review = %s", &out)
	}
	if p.procsCalls != 1 || len(p.peerCalls) != 1 || len(p.peerCalls[0]) != 1 || p.peerCalls[0][0] != pid {
		t.Fatalf("unexpected inspection: %+v", p)
	}
	for _, name := range []string{"driga.db", "collector.lock", "hint"} {
		if _, err := os.Stat(filepath.Join(os.Getenv("DRIGA_DATA_DIR"), name)); !os.IsNotExist(err) {
			t.Fatalf("read-only inspection wrote %s: %v", name, err)
		}
	}
}

func TestReclaimPIDNotPresentIsExplicit(t *testing.T) {
	readEnv(t)
	p := &reclaimReviewPlatform{procs: []model.Proc{{PID: int32(os.Getpid()), OwnerKnown: true, OwnerUID: 123}}}
	pid := int32(os.Getpid()) + 1
	var out bytes.Buffer
	if err := (ReclaimListCmd{PID: &pid}).Run(&Globals{JSON: true, Stdout: &out, CleanupPlatform: p}); err != nil {
		t.Fatal(err)
	}
	var envelope struct{ Data []reclaim.Opportunity }
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var target *reclaim.Opportunity
	for i := range envelope.Data {
		if envelope.Data[i].RootPID == pid {
			target = &envelope.Data[i]
		}
	}
	if target == nil || target.Status != "not_found" || target.Action != "" || !strings.Contains(target.Reason, "exit or identity could not be established") {
		t.Fatalf("ambiguous absence: %s", &out)
	}
}

func TestReclaimSimulatorsDoNotInspectProcesses(t *testing.T) {
	readEnv(t)
	var devices []string
	for i := range 6 {
		devices = append(devices, fmt.Sprintf(`{"udid":"device-%d","state":"Booted","name":"Fixture %d","lastUsedAt":"2026-11-01T00:00:00Z"}`, i, i))
	}
	var out bytes.Buffer
	calls := 0
	g := &Globals{JSON: true, Stdout: &out, CleanupPlatform: &reclaimReviewPlatform{}, ReclaimTool: func(ctx context.Context, binary string, args ...string) ([]byte, error) {
		calls++
		if binary != "xcrun" || strings.Join(args, " ") != "simctl list devices booted --json" {
			t.Fatalf("unexpected command: %s %v", binary, args)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded simulator command")
		}
		return []byte(`{"devices":{"runtime":[` + strings.Join(devices, ",") + `]}}`), nil
	}}
	if err := (ReclaimListCmd{Simulators: true}).Run(g); err != nil {
		t.Fatal(err)
	}
	var envelope struct{ Data []reclaim.Opportunity }
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(envelope.Data) != 6 {
		t.Fatalf("inventory: %s", &out)
	}
	for _, row := range envelope.Data {
		if row.Kind != "simulator" || row.Status != "needs_review" || row.ResourcesKnown || row.Action != "" || !row.StartedAt.IsZero() || row.Simulator.LastBootedAt != nil || row.Simulator.LastUsedAt == nil {
			t.Fatalf("invented authority or boot age: %+v", row)
		}
	}
}

func TestReclaimReviewArgumentValidation(t *testing.T) {
	for _, args := range [][]string{{"--pid", "0"}, {"--pid", "-1"}, {"--processes", "--sizes"}, {"--pid", "12", "--simulators"}} {
		var out, stderr bytes.Buffer
		if code := run(append([]string{"reclaim"}, args...), "test", nil, &out, &stderr); code != 2 {
			t.Fatalf("%v: code=%d stderr=%s", args, code, &stderr)
		}
	}
}

type resolvedReclaimPlatform struct {
	*reclaimReviewPlatform
	resolvedCalls int
	state         model.PeerState
}

func (p *resolvedReclaimPlatform) ResolvedPeers(_ context.Context, pids []int32) (map[int32]model.PeerState, error) {
	p.resolvedCalls++
	result := map[int32]model.PeerState{}
	for _, pid := range pids {
		result[pid] = p.state
	}
	return result, nil
}

func TestReclaimResolvesBrokerClientsBeforeRetainingReady(t *testing.T) {
	readEnv(t)
	start := time.Now().Add(-24 * time.Hour)
	pid := int32(os.Getpid()) + 10000
	broker := model.Proc{PID: pid, PPID: 1, StartTime: start, OwnerKnown: true, OwnerUID: 123, Exe: "/tools/node", Argv: []string{"node", "/tmp/app-server-broker.mjs"}, Cmdline: "node /tmp/app-server-broker.mjs", Cwd: t.TempDir(), RSS: 100, MemoryMeasure: "rss"}
	readDB(t, func(tx *store.Tx) error {
		if err := tx.UpsertMachine(model.Machine{OS: "linux"}); err != nil {
			return err
		}
		if err := tx.ReplaceProcs([]model.Proc{broker}); err != nil {
			return err
		}
		if err := tx.ReplaceOrphans([]model.Orphan{{ID: "broker", RootPID: pid, StartTime: start, PIDs: []int32{pid}, ProcCount: 1, Verified: true, Signature: "codex-app-server-broker"}}); err != nil {
			return err
		}
		if err := tx.SetSourceOK("procs", time.Now()); err != nil {
			return err
		}
		return tx.SetSourceOK("claude", time.Now())
	})
	p := &resolvedReclaimPlatform{state: model.PeerConnected, reclaimReviewPlatform: &reclaimReviewPlatform{procs: []model.Proc{{PID: int32(os.Getpid()), OwnerKnown: true, OwnerUID: 123}, broker}}}
	var out bytes.Buffer
	if err := (ReclaimListCmd{Processes: true}).Run(&Globals{JSON: true, Stdout: &out, CleanupPlatform: p}); err != nil {
		t.Fatal(err)
	}
	var envelope struct{ Data []reclaim.Opportunity }
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	for _, row := range envelope.Data {
		if row.RootPID == pid && (p.resolvedCalls != 1 || row.Status != "protected" || row.Action != "") {
			t.Fatalf("unresolved broker accepted: calls=%d row=%+v", p.resolvedCalls, row)
		}
	}
	// Directory uncertainty cannot be repaired by clientless socket evidence.
	p.state, p.cwdErr = model.PeerNone, errors.New("fixture cwd failure")
	out.Reset()
	if err := (ReclaimListCmd{Processes: true}).Run(&Globals{JSON: true, Stdout: &out, CleanupPlatform: p}); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	for _, row := range envelope.Data {
		if row.RootPID == pid && (row.Status == "ready" || row.Action != "") {
			t.Fatalf("directory uncertainty retained ready: %+v", row)
		}
	}
}

func TestReclaimInventoryOutputHelper(t *testing.T) {
	if os.Getenv("DRIGA_TEST_INVENTORY_OUTPUT") != "oversized" {
		return
	}
	_, _ = os.Stdout.Write(bytes.Repeat([]byte("x"), reclaim.MaxSimulatorInventoryBytes+8192))
	os.Exit(0)
}

func TestReclaimInventoryCaptureIsBounded(t *testing.T) {
	t.Setenv("DRIGA_TEST_INVENTORY_OUTPUT", "oversized")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := runReclaimInventory(ctx, os.Args[0], "-test.run=^TestReclaimInventoryOutputHelper$")
	if err == nil || out != nil || !strings.Contains(err.Error(), "output limit") {
		t.Fatalf("unbounded capture: bytes=%d err=%v", len(out), err)
	}
}
