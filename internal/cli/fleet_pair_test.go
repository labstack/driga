package cli

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestFleetPairCommandWiring(t *testing.T) {
	readEnv(t)
	for _, args := range [][]string{{"fleet", "enroll", "--help"}, {"fleet", "add", "--help"}, {"fleet", "revoke", "--help"}, {"fleet", "remove", "--help"}, {"fleet", "unpair", "--help"}, {"fleet", "rekey", "--help"}, {"machine", "reset-id", "--help"}} {
		var out, errs bytes.Buffer
		if code := run(args, "test", strings.NewReader(""), &out, &errs); code != 0 {
			t.Errorf("%v: %d %s", args, code, &errs)
		}
	}
}

func TestFleetRemoveKeepsRowsAndPurgeDeletesRemoteOnly(t *testing.T) {
	readEnv(t)
	st := readDB(t, func(tx *store.Tx) error {
		if _, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: "local"}}); err != nil {
			return err
		}
		remote := tx.ForMachine("remote")
		if err := remote.UpsertMachine(model.Machine{Hostname: "remote"}); err != nil {
			return err
		}
		_, err := remote.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: "remote"}})
		return err
	})
	dir := filepath.Join(config.DataDir(), "fleet")
	for _, purge := range []bool{false, true} {
		if err := fleet.SavePeers(dir, []fleet.Peer{{Name: "vm", MachineID: "remote"}}); err != nil {
			t.Fatal(err)
		}
		args := []string{"fleet", "unpair", "vm"}
		if purge {
			args = append(args, "--purge")
		}
		readRun(t, args...)
		ss, err := st.SessionsScoped(store.SessionFilter{IncludeEnded: true}, store.Scope{})
		if err != nil {
			t.Fatal(err)
		}
		want := 2
		if purge {
			want = 1
		}
		if len(ss) != want || ss[0].ExternalID != "local" {
			t.Fatal(ss)
		}
	}
}

func TestFleetIdentityMaintenanceRequiresStoppedCollector(t *testing.T) {
	readEnv(t)
	id, err := fleet.LoadOrCreateIdentity(filepath.Join(config.DataDir(), "fleet"))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := collector.Lock(config.DataDir())
	if err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if code := run([]string{"fleet", "rekey"}, "test", strings.NewReader(""), &out, &errs); code == 0 || !strings.Contains(errs.String(), "stop the collector") {
		t.Fatalf("%d %s", code, &errs)
	}
	lock.Close()
	readRun(t, "fleet", "rekey")
	fresh, err := fleet.LoadOrCreateIdentity(filepath.Join(config.DataDir(), "fleet"))
	if err != nil || fresh.DeviceID == id.DeviceID {
		t.Fatal(err)
	}
	readRun(t, "machine", "reset-id")
}

func TestAddShowsGrantedPermissions(t *testing.T) {
	for _, ack := range []bool{false, true} {
		t.Run(fmt.Sprint(ack), func(t *testing.T) {
			readEnv(t)
			peerDir := t.TempDir()
			st, err := store.Open(filepath.Join(peerDir, "driga.db"), store.ReadWrite)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			id, err := fleet.LoadOrCreateIdentity(filepath.Join(peerDir, "fleet"))
			if err != nil {
				t.Fatal(err)
			}
			if err := st.Tx(context.Background(), func(tx *store.Tx) error { return tx.UpsertMachine(model.Machine{Hostname: "peer"}) }); err != nil {
				t.Fatal(err)
			}
			viewerID, err := fleet.LoadOrCreateIdentity(filepath.Join(config.DataDir(), "fleet"))
			if err != nil {
				t.Fatal(err)
			}
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			srv := fleet.NewServer(st, id, peerDir, config.Default())
			done := make(chan error, 1)
			go func() { done <- srv.Serve(ctx, ln) }()
			defer func() {
				cancel()
				if err := <-done; err != nil {
					t.Error(err)
				}
			}()
			for {
				_, err = fleet.Enroll(ctx, peerDir, fleet.EnrollOpts{AllowID: viewerID.DeviceID, Ack: ack}, nil)
				if err == nil {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal(err)
				case <-time.After(time.Millisecond):
				}
			}
			var out, errs bytes.Buffer
			g := &Globals{Stdout: &out, Stderr: &errs, Stdin: strings.NewReader("")}
			if err := (FleetAddCmd{Addr: ln.Addr().String(), Name: "peer", ID: id.DeviceID}).Run(g); err != nil {
				t.Fatal(err)
			}
			want := "read ✓ · ack ✗"
			if ack {
				want = "read ✓ · ack ✓"
			}
			if !strings.Contains(out.String(), want) {
				t.Fatalf("got %q; want %q", out.String(), want)
			}
		})
	}
}
