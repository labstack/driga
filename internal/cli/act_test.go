package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
)

func TestKillRefusesYoung(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix path or action contract; Windows phase 1 has dedicated watch/refusal tests")
	}

	readEnv(t)
	readDB(t, func(tx *store.Tx) error {
		return tx.ReplaceOrphans([]model.Orphan{{ID: "young", RootPID: 1, StartTime: time.Now().Add(-time.Minute), Young: true}})
	})
	var out, errout bytes.Buffer
	if code := run([]string{"orphans", "kill", "young", "--force"}, "test", strings.NewReader("y\ny\n"), &out, &errout); code == 0 || !strings.Contains(errout.String(), "too young to kill (age 1m0s, threshold 10m0s)") {
		t.Fatalf("code=%d out=%s err=%s", code, &out, &errout)
	}
	if strings.Contains(out.String(), "Kill young?") {
		t.Fatal("offered young candidate for kill")
	}
}

// cliSeqPlatform serves fixed process snapshots and socket peers for kill tests.
type cliSeqPlatform struct {
	snaps [][]model.Proc
	calls int
	peers map[int32]bool
}

func (p *cliSeqPlatform) Procs(context.Context) ([]model.Proc, error) {
	s := p.snaps[min(p.calls, len(p.snaps)-1)]
	p.calls++
	return s, nil
}
func (p *cliSeqPlatform) Machine(context.Context) (model.MachineSample, error) {
	return model.MachineSample{}, nil
}
func (p *cliSeqPlatform) Cwds(context.Context, []int32) (map[int32]string, error) { return nil, nil }
func (p *cliSeqPlatform) Alive(int32, time.Time) bool                             { return false }
func (p *cliSeqPlatform) ExternalPeers(context.Context, []int32) (map[int32]bool, error) {
	return p.peers, nil
}

// Snapshot mode never accumulates two passes, so an unverified candidate is
// verified on demand instead of being refused outright.
func TestKillVerifiesUnverifiedOnDemand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix path or action contract; Windows phase 1 has dedicated watch/refusal tests")
	}

	start := time.Unix(1700000000, 0)
	tree := func(cpu float64) []model.Proc {
		return []model.Proc{{PID: 4242, PPID: 1, StartTime: start, CPUSeconds: cpu, Cmdline: "helper", Argv: []string{"helper"}}}
	}
	for _, tc := range []struct {
		name   string
		plat   *cliSeqPlatform
		prompt bool
		want   string
	}{
		{"busy", &cliSeqPlatform{snaps: [][]model.Proc{tree(1), tree(2)}}, false, "not idle: used 1.00s CPU"},
		{"connected", &cliSeqPlatform{snaps: [][]model.Proc{tree(1)}, peers: map[int32]bool{4242: true}}, false, "not idle: pid 4242 has a connection outside the tree"},
		{"quiet", &cliSeqPlatform{snaps: [][]model.Proc{tree(1)}}, true, "verified idle"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			readEnv(t)
			readDB(t, func(tx *store.Tx) error {
				return tx.ReplaceOrphans([]model.Orphan{{ID: "unverified", RootPID: 4242, StartTime: start, PIDs: []int32{4242}}})
			})
			oldPlat, oldWindow := actPlatform, verifyWindow
			actPlatform, verifyWindow = func() platform.Platform { return tc.plat }, time.Millisecond
			t.Cleanup(func() { actPlatform, verifyWindow = oldPlat, oldWindow })
			var out bytes.Buffer
			err := (OrphansKillCmd{ID: "unverified"}).Run(&Globals{Stdout: &out, Stderr: &bytes.Buffer{}, Stdin: strings.NewReader("n\n")})
			message := out.String()
			if err != nil {
				message += err.Error()
			}
			if !strings.Contains(message, tc.want) || strings.Contains(out.String(), "[y/N]") != tc.prompt {
				t.Fatalf("error=%v output=%s", err, &out)
			}
		})
	}
}

func TestAckWithoutDaemon(t *testing.T) {
	readEnv(t)
	var out, errout bytes.Buffer
	if err := (InboxAckCmd{ID: "item"}).Run(&Globals{Stdout: &out, Stderr: &errout}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "daemon not running — ack recorded, applies on next collect\n" {
		t.Fatal(out.String())
	}
	events, _, err := spool.Drain(config.SpoolDir(), nil)
	if err != nil || len(events) != 1 || events[0].Event != "ack" || events[0].Message != "item" || time.Since(events[0].TS) > time.Second {
		t.Fatalf("%+v %v", events, err)
	}
	if _, err := os.Stat(filepath.Join(config.DataDir(), "driga.db")); !os.IsNotExist(err) {
		t.Fatalf("ack created database: %v", err)
	}
}

func TestAckWithDaemonReceipt(t *testing.T) {
	readEnv(t)
	s := readDB(t, nil)
	lock, err := collector.Lock(config.DataDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lock.Close() })
	path := filepath.Join(config.DataDir(), "driga.lock")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			events, _, err := spool.Drain(config.SpoolDir(), nil)
			if err != nil {
				done <- err
				return
			}
			if len(events) > 0 {
				done <- s.SetMeta("ack:item", time.Now().Format(time.RFC3339Nano))
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		done <- context.DeadlineExceeded
	}()
	var out, errout bytes.Buffer
	code := run([]string{"inbox", "ack", "item"}, "test", strings.NewReader(""), &out, &errout)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if code != 0 || out.String() != "acked item\n" {
		t.Fatalf("code=%d out=%s err=%s", code, &out, &errout)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("lock disturbed: %q -> %q: %v", before, after, err)
	}
	if other, err := collector.Lock(config.DataDir()); err == nil {
		other.Close()
		t.Fatal("collector lock released")
	}
}

func TestAckAllUnacked(t *testing.T) {
	readEnv(t)
	now := time.Now()
	s := readDB(t, func(tx *store.Tx) error {
		for _, i := range []model.InboxItem{{ID: "one", TS: now}, {ID: "two", TS: now}, {ID: "seen", TS: now, AckedAt: &now}} {
			if err := tx.UpsertInboxItem(i); err != nil {
				return err
			}
		}
		return nil
	})
	readRun(t, "inbox", "ack", "--all")
	events, _, err := spool.Drain(config.SpoolDir(), nil)
	if err != nil || len(events) != 2 {
		t.Fatalf("%+v %v", events, err)
	}
	for _, e := range events {
		if e.Message == "seen" || e.Event != "ack" {
			t.Fatal(e)
		}
	}
	items, err := s.Inbox(time.Time{}, false)
	if err != nil || len(items) != 2 {
		t.Fatalf("reader changed DB: %+v %v", items, err)
	}
}

func TestActionArgumentsAndUnknownOrphan(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix path or action contract; Windows phase 1 has dedicated watch/refusal tests")
	}

	readEnv(t)
	readDB(t, nil)
	for _, args := range [][]string{{"inbox", "ack"}, {"inbox", "ack", "item", "--all"}, {"orphans", "kill"}, {"orphans", "kill", "missing"}, {"orphans", "kill", "missing", "--all"}} {
		var out, errout bytes.Buffer
		if code := run(args, "test", strings.NewReader("y\ny\n"), &out, &errout); code == 0 {
			t.Fatalf("accepted %v: %s", args, &out)
		}
	}
}

func TestKillConfirmation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix path or action contract; Windows phase 1 has dedicated watch/refusal tests")
	}

	readEnv(t)
	procs := []model.Proc{{PID: 1, Cmdline: "fixture", Argv: []string{"fixture"}, RSS: 1024}}
	candidates := []model.Orphan{{ID: "protected", RootPID: 1, StartTime: time.Now().Add(-time.Hour), PIDs: []int32{1}, RSSTotal: 1024, Verified: true}}
	for _, tc := range []struct {
		input         string
		force, second bool
	}{{"\n", false, false}, {"yes\n", false, false}, {"y\nn\n", true, true}} {
		var out bytes.Buffer
		err := (OrphansKillCmd{ID: "protected", Force: tc.force}).killCandidates(context.Background(), &Globals{Stdout: &out, Stderr: &bytes.Buffer{}, Stdin: strings.NewReader(tc.input)}, config.Default(), procs, candidates)
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{"driga orphans kill protected", "PID", "CMD", "RSS", "fixture", "~reclaim", "Kill protected? [y/N]"} {
			if !strings.Contains(out.String(), text) {
				t.Fatalf("missing %q: %s", text, &out)
			}
		}
		if strings.Contains(out.String(), "Really SIGKILL survivors? [y/N]") != tc.second {
			t.Fatal(out.String())
		}
	}
}

func TestKillAllPromptsPerCandidate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix path or action contract; Windows phase 1 has dedicated watch/refusal tests")
	}

	readEnv(t)
	candidates := []model.Orphan{{ID: "one", RootPID: 1, Verified: true}, {ID: "two", RootPID: 1, Verified: true}, {ID: "young", Young: true, StartTime: time.Now().Add(-time.Minute)}}
	var out, errout bytes.Buffer
	if err := (OrphansKillCmd{All: true}).killCandidates(context.Background(), &Globals{Stdout: &out, Stderr: &errout, Stdin: strings.NewReader("n\nn\ny\n")}, config.Default(), nil, candidates); err != nil {
		t.Fatalf("error=%v out=%s err=%s", err, &out, &errout)
	}
	if strings.Count(out.String(), "? [y/N]") != 2 || !strings.Contains(out.String(), "Kill one?") || !strings.Contains(out.String(), "Kill two?") || !strings.Contains(out.String(), "too young to kill") {
		t.Fatal(out.String())
	}
}

func TestAckReceiptTimeout(t *testing.T) {
	for _, missingDB := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-receipt", true: "missing-db"}[missingDB], func(t *testing.T) {
			readEnv(t)
			if !missingDB {
				readDB(t, nil)
			}
			lock, err := collector.Lock(config.DataDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { lock.Close() })
			start := time.Now()
			out, _ := readRun(t, "inbox", "ack", "item")
			if out != "ack recorded; daemon has not confirmed yet\n" || time.Since(start) > 5*time.Second {
				t.Fatalf("elapsed=%s output=%s", time.Since(start), out)
			}
			events, _, err := spool.Drain(config.SpoolDir(), nil)
			if err != nil || len(events) != 1 {
				t.Fatalf("%+v %v", events, err)
			}
		})
	}
}

func TestKillAllSkipsRemote(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix path or action contract; Windows phase 1 has dedicated watch/refusal tests")
	}

	readEnv(t)
	readDB(t, func(tx *store.Tx) error {
		if err := tx.ReplaceOrphans([]model.Orphan{{ID: "local", RootPID: 4242, Verified: true}}); err != nil {
			return err
		}
		remote := tx.ForMachine("remote")
		if err := remote.UpsertMachine(model.Machine{Name: "vm"}); err != nil {
			return err
		}
		return remote.ReplaceOrphans([]model.Orphan{{ID: "remote", RootPID: 4242, Verified: true}})
	})
	var out, errout bytes.Buffer
	// Decline the local candidate: this regression never signals any process.
	code := run([]string{"orphans", "kill", "--all"}, "test", strings.NewReader("n\n"), &out, &errout)
	if code != 0 || !strings.Contains(out.String(), "skipped 1 orphans on other machines") || !strings.Contains(out.String(), "Kill local?") || strings.Contains(out.String(), "Kill remote?") {
		t.Fatalf("code=%d out=%s err=%s", code, &out, &errout)
	}
}
