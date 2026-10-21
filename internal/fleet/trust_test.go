package fleet_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/labstack/driga/internal/fleet"
)

func TestTrustFilesAtomic0600(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fleet")
	if got, err := fleet.LoadPeers(dir); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	if got, err := fleet.LoadViewers(dir); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	at := time.Date(2026, 10, 18, 1, 2, 3, 0, time.UTC)
	peers := []fleet.Peer{{Name: "vm", Addr: "127.0.0.1:7411", DeviceID: fleet.DeviceID([]byte("p")), MachineID: "machine", Hostname: "vm", OS: "linux", PairedAt: at}}
	viewers := []fleet.Viewer{{Name: "viewer", DeviceID: fleet.DeviceID([]byte("v")), MachineID: "viewer-machine", Permissions: []string{"read", "ack"}, PairedAt: at, LastConnected: at, EnrolledFrom: "127.0.0.1"}}
	if err := fleet.SavePeers(dir, peers); err != nil {
		t.Fatal(err)
	}
	if err := fleet.SaveViewers(dir, viewers); err != nil {
		t.Fatal(err)
	}
	old, err := os.Open(filepath.Join(dir, "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	peers[0].Name = "renamed"
	if err := fleet.SavePeers(dir, peers); err != nil {
		t.Fatal(err)
	}
	oldInfo, _ := old.Stat()
	newInfo, _ := os.Stat(filepath.Join(dir, "peers.json"))
	if os.SameFile(oldInfo, newInfo) {
		t.Fatal("save overwrote the inode instead of renaming")
	}
	got, err := fleet.LoadPeers(dir)
	if err != nil || !reflect.DeepEqual(got, peers) {
		t.Fatal(got, err)
	}
	vg, err := fleet.LoadViewers(dir)
	if err != nil || !reflect.DeepEqual(vg, viewers) {
		t.Fatal(vg, err)
	}
	for _, name := range []string{"peers.json", "viewers.json"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("%s mode %o", name, info.Mode().Perm())
		}
	}
	info, _ := os.Stat(dir)
	if info.Mode().Perm() != 0700 {
		t.Fatal(info.Mode())
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("left temp files: %v %v", entries, err)
	}
	if err := os.Chmod(filepath.Join(dir, "peers.json"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := fleet.SavePeers(dir, nil); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Stat(filepath.Join(dir, "peers.json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("mode not restored")
	}
}

func TestTrustFilesRejectMalformed(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"peers.json", "viewers.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fleet.LoadPeers(dir); err == nil {
		t.Fatal("malformed peers accepted")
	}
	if _, err := fleet.LoadViewers(dir); err == nil {
		t.Fatal("malformed viewers accepted")
	}
}
