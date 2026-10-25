package collector

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLockRecordsOwner(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"app", "service", "manual"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			want := Owner{PID: os.Getpid(), Kind: kind, Executable: exe, Version: "test"}
			lock, err := Lock(dir, want)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			got, ok, err := ReadOwner(dir)
			if err != nil || !ok || got != want {
				t.Fatalf("owner: %+v, %v, %v; want %+v", got, ok, err, want)
			}
			data, err := os.ReadFile(filepath.Join(dir, "driga.lock"))
			if err != nil {
				t.Fatal(err)
			}
			var recorded Owner
			if err := json.Unmarshal(data, &recorded); err != nil || recorded != want {
				t.Fatalf("recorded owner: %+v, %v", recorded, err)
			}
			if _, err := Lock(dir, want); !errors.Is(err, ErrAlreadyRunning) {
				t.Fatalf("ReadOwner released holder's lock: %v", err)
			}
		})
	}
}

func TestReadOwnerWithoutHolder(t *testing.T) {
	dir := t.TempDir()
	if got, ok, err := ReadOwner(dir); err != nil || ok || got != (Owner{}) {
		t.Fatalf("missing lock: %+v, %v, %v", got, ok, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "driga.lock")); !os.IsNotExist(err) {
		t.Fatalf("ReadOwner created lock: %v", err)
	}
	lock, err := Lock(dir, Owner{Kind: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := ReadOwner(dir); err != nil || ok || got != (Owner{}) {
		t.Fatalf("stale owner: %+v, %v, %v", got, ok, err)
	}
	lock, err = Lock(dir, Owner{Kind: "manual"})
	if err != nil {
		t.Fatalf("ReadOwner left its probe locked: %v", err)
	}
	lock.Close()
}

func TestLockBusyNamesOwner(t *testing.T) {
	if dir := os.Getenv("DRIGA_COLLECTOR_LOCK_CHILD"); dir != "" {
		lock, err := Lock(dir, Owner{Kind: "manual"})
		if err == nil {
			lock.Close()
			t.Fatal("child acquired held lock")
		}
		if !errors.Is(err, ErrAlreadyRunning) || !strings.Contains(err.Error(), "app, pid ") {
			t.Fatalf("busy lock: %v", err)
		}
		fmt.Println(err)
		return
	}
	dir := t.TempDir()
	lock, err := Lock(dir, Owner{Kind: "app"})
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestLockBusyNamesOwner$", "-test.v")
	cmd.Env = append(os.Environ(), "DRIGA_COLLECTOR_LOCK_CHILD="+dir)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), fmt.Sprintf("app, pid %d", os.Getpid())) {
		t.Fatalf("child lock attempt: %v\n%s", err, out)
	}
}
