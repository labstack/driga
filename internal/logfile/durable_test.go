package logfile

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/labstack/driga/internal/filelock"
)

func fullLog(t *testing.T) (string, []byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "receipts.log")
	original := bytes.Repeat([]byte{'x'}, int(MaxSize))
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	return path, original
}

func TestDurableRotationProtectsReceiptUntilCopiedAndSynced(t *testing.T) {
	path, original := fullLog(t)
	var synced []string
	writer := Writer{Path: path, Durable: true}
	n, err := writer.write([]byte("new receipt\n"), func(f *os.File) error {
		// A different writer must be unable to rotate either receipt while its
		// durable acknowledgment is still pending, including during backup sync.
		competitor, err := os.OpenFile(path, os.O_RDWR, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer competitor.Close()
		lockErr := filelock.Lock(competitor, true)
		if lockErr == nil {
			filelock.Unlock(competitor)
			t.Fatal("receipt synchronization ran outside the cross-process lock")
		}
		if !filelock.Busy(lockErr) {
			t.Fatalf("unexpected lock error: %v", lockErr)
		}
		data, err := os.ReadFile(f.Name())
		if err != nil {
			t.Fatal(err)
		}
		if f.Name() == path+".1" {
			current, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(data, original) || !bytes.Equal(current, original) {
				t.Fatal("original receipt removed before backup synchronization")
			}
		} else if string(data) != "new receipt\n" {
			t.Fatalf("new receipt missing at synchronization: %q", data)
		}
		synced = append(synced, f.Name())
		return f.Sync()
	})
	if err != nil || n != len("new receipt\n") {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if len(synced) != 2 || synced[0] != path+".1" || synced[1] != path {
		t.Fatalf("durable receipt acknowledgment order: %v", synced)
	}
}

func TestDurableBackupSyncFailurePreservesCurrentReceipts(t *testing.T) {
	path, original := fullLog(t)
	unavailable := errors.New("sync unavailable")
	writer := Writer{Path: path, Durable: true}
	n, err := writer.write([]byte("new receipt\n"), func(f *os.File) error { return unavailable })
	if !errors.Is(err, unavailable) || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current, original) {
		t.Fatal("backup sync failure destroyed previously acknowledged receipts")
	}
}

func TestDurableAppendSyncFailureIsReturned(t *testing.T) {
	unavailable := errors.New("sync unavailable")
	writer := Writer{Path: filepath.Join(t.TempDir(), "receipts.log"), Durable: true}
	n, err := writer.write([]byte("receipt\n"), func(f *os.File) error { return unavailable })
	if !errors.Is(err, unavailable) || n != len("receipt\n") {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestOrdinaryLogsDoNotSynchronize(t *testing.T) {
	path, _ := fullLog(t)
	writer := Writer{Path: path}
	if _, err := writer.write([]byte("next\n"), func(f *os.File) error { t.Fatal("ordinary diagnostic log requested durable I/O"); return nil }); err != nil {
		t.Fatal(err)
	}
}
